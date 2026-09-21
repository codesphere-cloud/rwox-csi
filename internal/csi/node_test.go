package csi

import (
	"context"
	"path/filepath"
	"testing"

	csipb "github.com/container-storage-interface/spec/lib/go/csi"
	storagev1alpha1 "github.com/rwox-csi/rwox-csi/api/v1alpha1"
	"github.com/rwox-csi/rwox-csi/internal/kube"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/mount-utils"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestNodeStagesNFSAndPublishesReadonlyBind(t *testing.T) {
	v := &storagev1alpha1.RWXVolume{ObjectMeta: metav1.ObjectMeta{Name: "rwxvol-test", Generation: 2}, Spec: storagev1alpha1.RWXVolumeSpec{NFS: storagev1alpha1.NFSSpec{Version: "4.1", ExportPath: "/"}}, Status: storagev1alpha1.RWXVolumeStatus{Gateway: storagev1alpha1.GatewayStatus{DNSName: "rwxvol-test.rwx-csi-system.svc.cluster.local"}, Conditions: []metav1.Condition{{Type: storagev1alpha1.ConditionReady, Status: metav1.ConditionTrue, ObservedGeneration: 2, Reason: "Ready", Message: "ready"}}}}
	m := mount.NewFakeMounter(nil)
	s := &NodeServer{Client: fake.NewClientBuilder().WithScheme(kube.Scheme()).WithObjects(v).Build(), NodeID: "node-a", Mounter: m}
	root := t.TempDir()
	stage := filepath.Join(root, "stage")
	target := filepath.Join(root, "target")
	if _, err := s.NodeStageVolume(context.Background(), &csipb.NodeStageVolumeRequest{VolumeId: v.Name, StagingTargetPath: stage, VolumeCapability: rwxCapability()}); err != nil {
		t.Fatal(err)
	}
	log := m.GetLog()
	if len(log) != 1 || log[0].Source != "rwxvol-test.rwx-csi-system.svc.cluster.local:/" || log[0].FSType != "nfs4" {
		t.Fatalf("unexpected stage log: %#v", log)
	}
	if _, err := s.NodePublishVolume(context.Background(), &csipb.NodePublishVolumeRequest{VolumeId: v.Name, StagingTargetPath: stage, TargetPath: target, VolumeCapability: rwxCapability(), Readonly: true}); err != nil {
		t.Fatal(err)
	}
	foundRO := false
	for _, mp := range m.MountPoints {
		if mp.Path == target {
			for _, o := range mp.Opts {
				if o == "ro" {
					foundRO = true
				}
			}
		}
	}
	if !foundRO {
		t.Fatalf("target was not remounted read-only: %#v", m.MountPoints)
	}
}
