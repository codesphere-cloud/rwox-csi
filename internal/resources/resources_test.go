package resources

import (
	"strings"
	"testing"

	storagev1alpha1 "github.com/rwox-csi/rwox-csi/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func volume() *storagev1alpha1.RWXVolume {
	return &storagev1alpha1.RWXVolume{ObjectMeta: metav1.ObjectMeta{Name: "rwxvol-test", UID: "uid"}, Spec: storagev1alpha1.RWXVolumeSpec{Capacity: resource.MustParse("10Gi"), Backing: storagev1alpha1.BackingSpec{StorageClassName: "fast"}}}
}
func TestResourceContract(t *testing.T) {
	v := volume()
	n := NamesFor(v.Name)
	if n.PVC != "rwxvol-test-backing" || n.ConfigMap != "rwxvol-test-ganesha" || n.StatefulSet != "rwxvol-test-gateway" || n.Service != "rwxvol-test" {
		t.Fatalf("unexpected names: %#v", n)
	}
	pvc := PVC(v, "rwx-csi-system")
	if *pvc.Spec.StorageClassName != "fast" || pvc.Spec.AccessModes[0] != "ReadWriteOnce" {
		t.Fatalf("unexpected PVC: %#v", pvc.Spec)
	}
	sts := StatefulSet(v, "rwx-csi-system", "ganesha:test")
	if *sts.Spec.Replicas != 1 || sts.Spec.Template.Spec.Volumes[0].PersistentVolumeClaim.ClaimName != n.PVC {
		t.Fatalf("unexpected StatefulSet: %#v", sts.Spec)
	}
	ganesha := sts.Spec.Template.Spec.Containers[0]
	if len(ganesha.Command) != 2 || ganesha.Command[0] != "/bin/bash" || ganesha.Command[1] != "-ec" {
		t.Fatalf("unexpected Ganesha command: %#v", ganesha.Command)
	}
	if len(ganesha.Args) != 1 || !strings.Contains(ganesha.Args[0], "exec /usr/local/bin/ganesha.nfsd") {
		t.Fatalf("unexpected Ganesha arguments: %#v", ganesha.Args)
	}
	for _, prerequisite := range []string{"rpcbind", "rpc.statd", "dbus-daemon"} {
		if !strings.Contains(ganesha.Args[0], prerequisite) {
			t.Errorf("Ganesha startup command does not start %s", prerequisite)
		}
	}
	if Service(v, "rwx-csi-system").Spec.Ports[0].Port != 2049 {
		t.Fatal("NFS service does not expose 2049")
	}
}
func TestGaneshaSecurityContract(t *testing.T) {
	cfg := GaneshaConfig()
	for _, want := range []string{"Protocols = 4", "SecType = sys", "Squash = No_Root_Squash", "Path = \"/data\""} {
		if !strings.Contains(cfg, want) {
			t.Errorf("config missing %q", want)
		}
	}
}
func TestServiceDNS(t *testing.T) {
	if got := ServiceDNS("vol", "ns", ""); got != "vol.ns.svc.cluster.local" {
		t.Fatalf("got %q", got)
	}
}
