package resources

import (
	"fmt"

	storagev1alpha1 "github.com/rwox-csi/rwox-csi/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
)

const (
	VolumeLabel         = "storage.example.io/volume"
	ManagedByLabel      = "app.kubernetes.io/managed-by"
	DefaultGaneshaImage = "ghcr.io/kubernetes-sigs/nfs-ganesha:v6.5"
	ganeshaStartup      = `
/usr/sbin/rpcinfo 127.0.0.1 >/dev/null 2>&1 || /usr/sbin/rpcbind -w
/usr/sbin/rpc.statd --port 662
/usr/bin/dbus-daemon --system --nopidfile
exec /usr/local/bin/ganesha.nfsd -F -L /dev/stdout -p /var/run/ganesha.pid -f /etc/ganesha/ganesha.conf
`
)

type Names struct{ PVC, ConfigMap, StatefulSet, Service string }

func NamesFor(volume string) Names {
	return Names{PVC: volume + "-backing", ConfigMap: volume + "-ganesha", StatefulSet: volume + "-gateway", Service: volume}
}
func Labels(volume string) map[string]string {
	return map[string]string{VolumeLabel: volume, ManagedByLabel: "rwox-csi"}
}

func owner(v *storagev1alpha1.RWXVolume) []metav1.OwnerReference {
	return []metav1.OwnerReference{*metav1.NewControllerRef(v, schema.GroupVersionKind{Group: storagev1alpha1.GroupVersion.Group, Version: storagev1alpha1.GroupVersion.Version, Kind: "RWXVolume"})}
}

func PVC(v *storagev1alpha1.RWXVolume, namespace string) *corev1.PersistentVolumeClaim {
	n := NamesFor(v.Name)
	mode := corev1.PersistentVolumeFilesystem
	return &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: n.PVC, Namespace: namespace, Labels: Labels(v.Name), OwnerReferences: owner(v)}, Spec: corev1.PersistentVolumeClaimSpec{AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}, VolumeMode: &mode, StorageClassName: ptr.To(v.Spec.Backing.StorageClassName), Resources: corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceStorage: v.Spec.Capacity.DeepCopy()}}}}
}

func backingPath(v *storagev1alpha1.RWXVolume) string {
	if v.Spec.NFS.BackingDirectory == "" {
		return "/data"
	}
	return "/data/" + v.Spec.NFS.BackingDirectory
}

func GaneshaConfig(v *storagev1alpha1.RWXVolume) string {
	return fmt.Sprintf(`NFS_CORE_PARAM {
  Protocols = 4;
}
EXPORT_DEFAULTS {
  Access_Type = RW;
  Protocols = 4;
  Transports = TCP;
  SecType = sys;
  Squash = No_Root_Squash;
}
EXPORT {
  Export_Id = 1;
  Path = %q;
  Pseudo = "/";
  FSAL { Name = VFS; }
}
`, backingPath(v))
}

func ConfigMap(v *storagev1alpha1.RWXVolume, namespace string) *corev1.ConfigMap {
	n := NamesFor(v.Name)
	return &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: n.ConfigMap, Namespace: namespace, Labels: Labels(v.Name), OwnerReferences: owner(v)}, Data: map[string]string{"ganesha.conf": GaneshaConfig(v)}}
}

func Service(v *storagev1alpha1.RWXVolume, namespace string) *corev1.Service {
	n := NamesFor(v.Name)
	return &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: n.Service, Namespace: namespace, Labels: Labels(v.Name), OwnerReferences: owner(v)}, Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeClusterIP, Selector: Labels(v.Name), Ports: []corev1.ServicePort{{Name: "nfs", Port: 2049, Protocol: corev1.ProtocolTCP}}}}
}

func StatefulSet(v *storagev1alpha1.RWXVolume, namespace, image string) *appsv1.StatefulSet {
	n := NamesFor(v.Name)
	labels := Labels(v.Name)
	path := backingPath(v)
	startup := ""
	if v.Spec.NFS.BackingDirectory != "" {
		startup += fmt.Sprintf("mkdir -p %q\n", path)
	}
	if v.Spec.NFS.DirectoryMode != "" {
		startup += fmt.Sprintf("find %q -xdev -type d -exec chmod %s {} +\n", path, v.Spec.NFS.DirectoryMode)
	}
	startup += ganeshaStartup
	return &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: n.StatefulSet, Namespace: namespace, Labels: labels, OwnerReferences: owner(v)}, Spec: appsv1.StatefulSetSpec{ServiceName: n.Service, Replicas: ptr.To[int32](1), Selector: &metav1.LabelSelector{MatchLabels: labels}, PodManagementPolicy: appsv1.ParallelPodManagement, Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: labels}, Spec: corev1.PodSpec{TerminationGracePeriodSeconds: ptr.To[int64](30), Containers: []corev1.Container{{Name: "ganesha", Image: image, ImagePullPolicy: corev1.PullIfNotPresent, Command: []string{"/bin/bash", "-ec"}, Args: []string{startup}, SecurityContext: &corev1.SecurityContext{Privileged: ptr.To(true)}, Ports: []corev1.ContainerPort{{Name: "nfs", ContainerPort: 2049, Protocol: corev1.ProtocolTCP}}, ReadinessProbe: &corev1.Probe{ProbeHandler: corev1.ProbeHandler{Exec: &corev1.ExecAction{Command: []string{"sh", "-ec", "rpcinfo -t 127.0.0.1 nfs 4 >/dev/null"}}}, InitialDelaySeconds: 2, PeriodSeconds: 3, FailureThreshold: 10}, Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("25m"), corev1.ResourceMemory: resource.MustParse("64Mi")}}, VolumeMounts: []corev1.VolumeMount{{Name: "data", MountPath: "/data"}, {Name: "config", MountPath: "/etc/ganesha/ganesha.conf", SubPath: "ganesha.conf", ReadOnly: true}}}}, Volumes: []corev1.Volume{{Name: "data", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: n.PVC}}}, {Name: "config", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: n.ConfigMap}}}}}}}}}
}

func ServiceDNS(name, namespace, clusterDomain string) string {
	if clusterDomain == "" {
		clusterDomain = "cluster.local"
	}
	return fmt.Sprintf("%s.%s.svc.%s", name, namespace, clusterDomain)
}
