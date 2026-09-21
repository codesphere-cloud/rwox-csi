package csi

import (
	"context"
	"testing"

	csipb "github.com/container-storage-interface/spec/lib/go/csi"
	storagev1alpha1 "github.com/rwox-csi/rwox-csi/api/v1alpha1"
	"github.com/rwox-csi/rwox-csi/internal/kube"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func rwxCapability() *csipb.VolumeCapability {
	return &csipb.VolumeCapability{AccessType: &csipb.VolumeCapability_Mount{Mount: &csipb.VolumeCapability_MountVolume{}}, AccessMode: &csipb.VolumeCapability_AccessMode{Mode: csipb.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER}}
}
func TestCreateVolumeIsAsynchronousAndIdempotent(t *testing.T) {
	ctx := context.Background()
	cl := fake.NewClientBuilder().WithScheme(kube.Scheme()).WithStatusSubresource(&storagev1alpha1.RWXVolume{}).Build()
	s := &ControllerServer{Client: cl}
	req := &csipb.CreateVolumeRequest{Name: "pvc-123", CapacityRange: &csipb.CapacityRange{RequiredBytes: 1 << 30}, VolumeCapabilities: []*csipb.VolumeCapability{rwxCapability()}, Parameters: map[string]string{BackingStorageClassParameter: "fast-rwo"}}
	if _, err := s.CreateVolume(ctx, req); status.Code(err) != codes.Aborted {
		t.Fatalf("first call code=%s err=%v", status.Code(err), err)
	}
	v := &storagev1alpha1.RWXVolume{}
	if err := cl.Get(ctx, types.NamespacedName{Name: volumeName(req.Name)}, v); err != nil {
		t.Fatal(err)
	}
	if v.Spec.Backing.StorageClassName != "fast-rwo" {
		t.Fatalf("backing class=%q", v.Spec.Backing.StorageClassName)
	}
	cap := resource.MustParse("1Gi")
	v.Status.Capacity = &cap
	v.Status.Conditions = []metav1.Condition{{Type: storagev1alpha1.ConditionReady, Status: metav1.ConditionTrue, ObservedGeneration: v.Generation, Reason: "Ready", Message: "ready"}}
	if err := cl.Status().Update(ctx, v); err != nil {
		t.Fatal(err)
	}
	resp, err := s.CreateVolume(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Volume.VolumeId != v.Name || resp.Volume.CapacityBytes != 1<<30 {
		t.Fatalf("unexpected response: %#v", resp.Volume)
	}
}
func TestCreateVolumeRejectsUnsupportedCapability(t *testing.T) {
	s := &ControllerServer{Client: fake.NewClientBuilder().WithScheme(kube.Scheme()).Build()}
	cap := rwxCapability()
	cap.AccessMode.Mode = csipb.VolumeCapability_AccessMode_SINGLE_NODE_WRITER
	_, err := s.CreateVolume(context.Background(), &csipb.CreateVolumeRequest{Name: "x", VolumeCapabilities: []*csipb.VolumeCapability{cap}, Parameters: map[string]string{BackingStorageClassParameter: "fast"}})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("code=%s err=%v", status.Code(err), err)
	}
}
func TestValidateVolumeCapabilities(t *testing.T) {
	v := &storagev1alpha1.RWXVolume{ObjectMeta: metav1.ObjectMeta{Name: "rwxvol-test"}}
	s := &ControllerServer{Client: fake.NewClientBuilder().WithScheme(kube.Scheme()).WithObjects(v).Build()}
	resp, err := s.ValidateVolumeCapabilities(context.Background(), &csipb.ValidateVolumeCapabilitiesRequest{VolumeId: v.Name, VolumeCapabilities: []*csipb.VolumeCapability{rwxCapability()}})
	if err != nil || resp.Confirmed == nil {
		t.Fatalf("resp=%#v err=%v", resp, err)
	}
}
