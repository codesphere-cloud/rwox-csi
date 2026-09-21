package operator

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	storagev1alpha1 "github.com/rwox-csi/rwox-csi/api/v1alpha1"
	"github.com/rwox-csi/rwox-csi/internal/resources"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apiMeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

var (
	reconcileDuration = prometheus.NewHistogram(prometheus.HistogramOpts{Name: "rwox_reconcile_duration_seconds", Help: "RWXVolume reconcile latency."})
	reconcileErrors   = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "rwox_reconcile_errors_total", Help: "RWXVolume reconciliation errors."}, []string{"reason"})
)

func init() { prometheus.MustRegister(reconcileDuration, reconcileErrors) }

type Reconciler struct {
	client.Client
	Namespace     string
	ClusterDomain string
	GaneshaImage  string
}

func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).For(&storagev1alpha1.RWXVolume{}).Owns(&corev1.PersistentVolumeClaim{}).Owns(&corev1.ConfigMap{}).Owns(&corev1.Service{}).Owns(&appsv1.StatefulSet{}).Complete(r)
}

func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	start := time.Now()
	defer func() { reconcileDuration.Observe(time.Since(start).Seconds()) }()
	log := ctrl.LoggerFrom(ctx).WithValues("rwxvolume", req.Name)
	v := &storagev1alpha1.RWXVolume{}
	if err := r.Get(ctx, types.NamespacedName{Name: req.Name}, v); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !v.DeletionTimestamp.IsZero() {
		return r.finalize(ctx, v)
	}
	if !controllerutil.ContainsFinalizer(v, storagev1alpha1.FinalizerName) {
		controllerutil.AddFinalizer(v, storagev1alpha1.FinalizerName)
		if err := r.Update(ctx, v); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}
	if v.Spec.Backing.StorageClassName == "" {
		return r.permanent(ctx, v, "MissingBackingStorageClass", "spec.backing.storageClassName is required")
	}
	sc := &storagev1.StorageClass{}
	if err := r.Get(ctx, types.NamespacedName{Name: v.Spec.Backing.StorageClassName}, sc); err != nil {
		if apierrors.IsNotFound(err) {
			return r.permanent(ctx, v, "BackingStorageClassNotFound", fmt.Sprintf("StorageClass %q does not exist", v.Spec.Backing.StorageClassName))
		}
		return r.transient(ctx, v, "StorageClassLookupFailed", err)
	}
	if err := r.ensureConfigMap(ctx, resources.ConfigMap(v, r.Namespace)); err != nil {
		return r.transient(ctx, v, "ConfigMapFailed", err)
	}
	if err := r.ensurePVC(ctx, v); err != nil {
		return r.transient(ctx, v, "BackingPVCFailed", err)
	}
	if err := r.ensureService(ctx, resources.Service(v, r.Namespace)); err != nil {
		return r.transient(ctx, v, "ServiceFailed", err)
	}
	if err := r.ensureStatefulSet(ctx, v); err != nil {
		return r.transient(ctx, v, "GatewayFailed", err)
	}

	n := resources.NamesFor(v.Name)
	pvc := &corev1.PersistentVolumeClaim{}
	sts := &appsv1.StatefulSet{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: r.Namespace, Name: n.PVC}, pvc); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.Get(ctx, types.NamespacedName{Namespace: r.Namespace, Name: n.StatefulSet}, sts); err != nil {
		return ctrl.Result{}, err
	}
	pvcReady := pvc.Status.Phase == corev1.ClaimBound
	gatewayReady := sts.Status.ReadyReplicas == 1
	exportReady := pvcReady && gatewayReady // readinessProbe performs an NFSv4 RPC exchange.
	phase := "Provisioning"
	if exportReady {
		phase = "Ready"
	}
	cap := v.Spec.Capacity.DeepCopy()
	if q, ok := pvc.Status.Capacity[corev1.ResourceStorage]; ok {
		cap = q.DeepCopy()
	}
	base := v.DeepCopy()
	v.Status.Phase = phase
	v.Status.ObservedGeneration = v.Generation
	v.Status.Failure = nil
	v.Status.Capacity = &cap
	v.Status.BackingPVC = storagev1alpha1.ObjectReference{Namespace: r.Namespace, Name: n.PVC}
	v.Status.Gateway = storagev1alpha1.GatewayStatus{ServiceName: n.Service, StatefulSetName: n.StatefulSet, DNSName: resources.ServiceDNS(n.Service, r.Namespace, r.ClusterDomain)}
	setCondition(v, storagev1alpha1.ConditionBackingPVCReady, pvcReady, "PVCBound", "waiting for backing PVC to bind")
	setCondition(v, storagev1alpha1.ConditionGatewayReady, gatewayReady, "GatewayAvailable", "waiting for gateway StatefulSet")
	setCondition(v, storagev1alpha1.ConditionExportReady, exportReady, "NFSResponding", "waiting for NFSv4 RPC readiness probe")
	setCondition(v, storagev1alpha1.ConditionReady, exportReady, "ResourcesReady", "volume infrastructure is not ready")
	if err := r.Status().Patch(ctx, v, client.MergeFrom(base)); err != nil {
		return ctrl.Result{}, err
	}
	log.Info("reconciled volume", "phase", phase)
	if !exportReady {
		return ctrl.Result{RequeueAfter: 3 * time.Second}, nil
	}
	return ctrl.Result{}, nil
}

func (r *Reconciler) ensureConfigMap(ctx context.Context, desired *corev1.ConfigMap) error {
	current := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: desired.Name, Namespace: desired.Namespace}}
	_, err := controllerutil.CreateOrPatch(ctx, r.Client, current, func() error {
		current.Labels = desired.Labels
		current.OwnerReferences = desired.OwnerReferences
		current.Data = desired.Data
		return nil
	})
	return err
}

func (r *Reconciler) ensureService(ctx context.Context, desired *corev1.Service) error {
	current := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: desired.Name, Namespace: desired.Namespace}}
	_, err := controllerutil.CreateOrPatch(ctx, r.Client, current, func() error {
		current.Labels = desired.Labels
		current.OwnerReferences = desired.OwnerReferences
		current.Spec.Type = desired.Spec.Type
		current.Spec.Selector = desired.Spec.Selector
		current.Spec.Ports = desired.Spec.Ports
		return nil
	})
	return err
}

func (r *Reconciler) ensurePVC(ctx context.Context, v *storagev1alpha1.RWXVolume) error {
	desired := resources.PVC(v, r.Namespace)
	current := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: desired.Name, Namespace: desired.Namespace}}
	_, err := controllerutil.CreateOrPatch(ctx, r.Client, current, func() error {
		if current.CreationTimestamp.IsZero() {
			current.Labels = desired.Labels
			current.OwnerReferences = desired.OwnerReferences
			current.Spec = desired.Spec
			return nil
		}
		want := desired.Spec.Resources.Requests[corev1.ResourceStorage]
		have := current.Spec.Resources.Requests[corev1.ResourceStorage]
		if want.Cmp(have) > 0 {
			current.Spec.Resources.Requests[corev1.ResourceStorage] = want
		}
		return nil
	})
	return err
}

func (r *Reconciler) ensureStatefulSet(ctx context.Context, v *storagev1alpha1.RWXVolume) error {
	d := resources.StatefulSet(v, r.Namespace, r.GaneshaImage)
	c := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: d.Name, Namespace: d.Namespace}}
	_, err := controllerutil.CreateOrPatch(ctx, r.Client, c, func() error { c.Labels = d.Labels; c.OwnerReferences = d.OwnerReferences; c.Spec = d.Spec; return nil })
	return err
}

func (r *Reconciler) finalize(ctx context.Context, v *storagev1alpha1.RWXVolume) (ctrl.Result, error) {
	n := resources.NamesFor(v.Name)
	sts := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: n.StatefulSet, Namespace: r.Namespace}}
	if err := r.Delete(ctx, sts); err != nil && !apierrors.IsNotFound(err) {
		return ctrl.Result{}, err
	}
	if err := r.Get(ctx, client.ObjectKeyFromObject(sts), sts); err == nil {
		return ctrl.Result{RequeueAfter: time.Second}, nil
	} else if !apierrors.IsNotFound(err) {
		return ctrl.Result{}, err
	}
	objects := []client.Object{&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: n.Service, Namespace: r.Namespace}}, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: n.ConfigMap, Namespace: r.Namespace}}, &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: n.PVC, Namespace: r.Namespace}}}
	for _, o := range objects {
		if err := r.Delete(ctx, o); err != nil && !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
	}
	for _, o := range objects {
		if err := r.Get(ctx, client.ObjectKeyFromObject(o), o); err == nil {
			return ctrl.Result{RequeueAfter: time.Second}, nil
		} else if !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
	}
	controllerutil.RemoveFinalizer(v, storagev1alpha1.FinalizerName)
	return ctrl.Result{}, r.Update(ctx, v)
}

func setCondition(v *storagev1alpha1.RWXVolume, t string, ok bool, reason, message string) {
	s := metav1.ConditionFalse
	if ok {
		s = metav1.ConditionTrue
		message = "ready"
	}
	apiMeta.SetStatusCondition(&v.Status.Conditions, metav1.Condition{Type: t, Status: s, ObservedGeneration: v.Generation, Reason: reason, Message: message})
}
func (r *Reconciler) permanent(ctx context.Context, v *storagev1alpha1.RWXVolume, reason, message string) (ctrl.Result, error) {
	reconcileErrors.WithLabelValues(reason).Inc()
	base := v.DeepCopy()
	v.Status.Phase = "Failed"
	v.Status.ObservedGeneration = v.Generation
	v.Status.Failure = &storagev1alpha1.FailureStatus{Class: "Permanent", Reason: reason, Message: message}
	setCondition(v, storagev1alpha1.ConditionReady, false, reason, message)
	return ctrl.Result{}, r.Status().Patch(ctx, v, client.MergeFrom(base))
}
func (r *Reconciler) transient(ctx context.Context, v *storagev1alpha1.RWXVolume, reason string, err error) (ctrl.Result, error) {
	reconcileErrors.WithLabelValues(reason).Inc()
	base := v.DeepCopy()
	v.Status.Phase = "Provisioning"
	v.Status.Failure = &storagev1alpha1.FailureStatus{Class: "Transient", Reason: reason, Message: err.Error()}
	setCondition(v, storagev1alpha1.ConditionReady, false, reason, err.Error())
	patchErr := r.Status().Patch(ctx, v, client.MergeFrom(base))
	return ctrl.Result{}, errors.Join(err, patchErr)
}
