package operator

import (
	"context"
	"testing"

	storagev1alpha1 "github.com/rwox-csi/rwox-csi/api/v1alpha1"
	"github.com/rwox-csi/rwox-csi/internal/kube"
	"github.com/rwox-csi/rwox-csi/internal/resources"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestReconcileCreatesOneResourceOfEachKind(t *testing.T) {
	ctx := context.Background()
	v := &storagev1alpha1.RWXVolume{ObjectMeta: metav1.ObjectMeta{Name: "rwxvol-test", UID: "uid"}, Spec: storagev1alpha1.RWXVolumeSpec{Capacity: resource.MustParse("1Gi"), Backing: storagev1alpha1.BackingSpec{StorageClassName: "fast"}, NFS: storagev1alpha1.NFSSpec{Version: "4.1", ExportPath: "/"}}}
	sc := &storagev1.StorageClass{ObjectMeta: metav1.ObjectMeta{Name: "fast"}}
	cl := fake.NewClientBuilder().WithScheme(kube.Scheme()).WithStatusSubresource(&storagev1alpha1.RWXVolume{}, &corev1.PersistentVolumeClaim{}, &appsv1.StatefulSet{}).WithObjects(v, sc).Build()
	r := &Reconciler{Client: cl, Namespace: "rwx-csi-system", ClusterDomain: "cluster.local", GaneshaImage: "ganesha:test"}
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: v.Name}}
	if _, err := r.Reconcile(ctx, req); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Reconcile(ctx, req); err != nil {
		t.Fatal(err)
	}
	n := resources.NamesFor(v.Name)
	checks := []struct {
		name string
		obj  any
	}{{n.PVC, &corev1.PersistentVolumeClaim{}}, {n.ConfigMap, &corev1.ConfigMap{}}, {n.Service, &corev1.Service{}}, {n.StatefulSet, &appsv1.StatefulSet{}}}
	for _, check := range checks {
		key := types.NamespacedName{Namespace: r.Namespace, Name: check.name}
		switch o := check.obj.(type) {
		case *corev1.PersistentVolumeClaim:
			if err := cl.Get(ctx, key, o); err != nil {
				t.Error(err)
			}
		case *corev1.ConfigMap:
			if err := cl.Get(ctx, key, o); err != nil {
				t.Error(err)
			}
		case *corev1.Service:
			if err := cl.Get(ctx, key, o); err != nil {
				t.Error(err)
			}
		case *appsv1.StatefulSet:
			if err := cl.Get(ctx, key, o); err != nil {
				t.Error(err)
			}
		}
	}
}
