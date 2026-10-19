package csi

import (
	"context"
	"testing"
	"time"

	csipb "github.com/container-storage-interface/spec/lib/go/csi"
	storagev1alpha1 "github.com/rwox-csi/rwox-csi/api/v1alpha1"
	"github.com/rwox-csi/rwox-csi/internal/kube"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func rwxCapability() *csipb.VolumeCapability {
	return &csipb.VolumeCapability{AccessType: &csipb.VolumeCapability_Mount{Mount: &csipb.VolumeCapability_MountVolume{}}, AccessMode: &csipb.VolumeCapability_AccessMode{Mode: csipb.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER}}
}

type readyOnCreateClient struct {
	client.Client
}

func (c *readyOnCreateClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	if err := c.Client.Create(ctx, obj, opts...); err != nil {
		return err
	}
	v, ok := obj.(*storagev1alpha1.RWXVolume)
	if !ok {
		return nil
	}
	stored := &storagev1alpha1.RWXVolume{}
	if err := c.Client.Get(ctx, client.ObjectKeyFromObject(v), stored); err != nil {
		return err
	}
	cap := resource.MustParse("1Gi")
	stored.Status.Capacity = &cap
	stored.Status.Conditions = []metav1.Condition{{Type: storagev1alpha1.ConditionReady, Status: metav1.ConditionTrue, ObservedGeneration: stored.Generation, Reason: "Ready", Message: "ready"}}
	return c.Client.Status().Update(ctx, stored)
}

func TestCreateVolumeWaitsUntilReadyAndIsIdempotent(t *testing.T) {
	ctx := context.Background()
	cl := fake.NewClientBuilder().WithScheme(kube.Scheme()).WithStatusSubresource(&storagev1alpha1.RWXVolume{}).Build()
	s := &ControllerServer{Client: &readyOnCreateClient{Client: cl}, ProvisioningPollInterval: time.Millisecond}
	req := &csipb.CreateVolumeRequest{Name: "pvc-123", CapacityRange: &csipb.CapacityRange{RequiredBytes: 1 << 30}, VolumeCapabilities: []*csipb.VolumeCapability{rwxCapability()}, Parameters: map[string]string{BackingStorageClassParameter: "fast-rwo", DirectoryModeParameter: "0777", DirectoryUIDParameter: "1501", DirectoryGIDParameter: "1010", BackingDirectoryParameter: "export"}}
	resp, err := s.CreateVolume(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	v := &storagev1alpha1.RWXVolume{}
	if err := cl.Get(ctx, types.NamespacedName{Name: volumeName(req.Name)}, v); err != nil {
		t.Fatal(err)
	}
	if v.Spec.Backing.StorageClassName != "fast-rwo" {
		t.Fatalf("backing class=%q", v.Spec.Backing.StorageClassName)
	}
	if v.Spec.NFS.DirectoryMode != "0777" {
		t.Fatalf("directory mode=%q", v.Spec.NFS.DirectoryMode)
	}
	if v.Spec.NFS.DirectoryUID != "1501" || v.Spec.NFS.DirectoryGID != "1010" {
		t.Fatalf("directory owner=%s:%s", v.Spec.NFS.DirectoryUID, v.Spec.NFS.DirectoryGID)
	}
	if v.Spec.NFS.BackingDirectory != "export" {
		t.Fatalf("backing directory=%q", v.Spec.NFS.BackingDirectory)
	}
	if resp.Volume.VolumeId != v.Name || resp.Volume.CapacityBytes != 1<<30 {
		t.Fatalf("unexpected response: %#v", resp.Volume)
	}
	resp, err = s.CreateVolume(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Volume.VolumeId != v.Name || resp.Volume.CapacityBytes != 1<<30 {
		t.Fatalf("unexpected response: %#v", resp.Volume)
	}
}

func TestCreateVolumeDeadlineLeavesProvisioningRunning(t *testing.T) {
	cl := fake.NewClientBuilder().WithScheme(kube.Scheme()).WithStatusSubresource(&storagev1alpha1.RWXVolume{}).Build()
	s := &ControllerServer{Client: cl, ProvisioningPollInterval: time.Millisecond}
	req := &csipb.CreateVolumeRequest{Name: "pvc-pending", VolumeCapabilities: []*csipb.VolumeCapability{rwxCapability()}, Parameters: map[string]string{BackingStorageClassParameter: "fast-rwo"}}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := s.CreateVolume(ctx, req)
	if status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("code=%s err=%v", status.Code(err), err)
	}
	v := &storagev1alpha1.RWXVolume{}
	if err := cl.Get(context.Background(), types.NamespacedName{Name: volumeName(req.Name)}, v); err != nil {
		t.Fatalf("provisioning resource was not retained: %v", err)
	}
}
func TestCreateVolumeRejectsInvalidDirectoryMode(t *testing.T) {
	s := &ControllerServer{Client: fake.NewClientBuilder().WithScheme(kube.Scheme()).Build()}
	_, err := s.CreateVolume(context.Background(), &csipb.CreateVolumeRequest{Name: "x", VolumeCapabilities: []*csipb.VolumeCapability{rwxCapability()}, Parameters: map[string]string{BackingStorageClassParameter: "fast", DirectoryModeParameter: "0777; touch /tmp/pwned"}})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("code=%s err=%v", status.Code(err), err)
	}
}
func TestCreateVolumeRejectsInvalidDirectoryOwner(t *testing.T) {
	s := &ControllerServer{Client: fake.NewClientBuilder().WithScheme(kube.Scheme()).Build()}
	for parameter, value := range map[string]string{DirectoryUIDParameter: "-1", DirectoryGIDParameter: "4294967296"} {
		_, err := s.CreateVolume(context.Background(), &csipb.CreateVolumeRequest{Name: "x", VolumeCapabilities: []*csipb.VolumeCapability{rwxCapability()}, Parameters: map[string]string{BackingStorageClassParameter: "fast", parameter: value}})
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("parameter=%s code=%s err=%v", parameter, status.Code(err), err)
		}
	}
}
func TestCreateVolumeRejectsInvalidBackingDirectory(t *testing.T) {
	s := &ControllerServer{Client: fake.NewClientBuilder().WithScheme(kube.Scheme()).Build()}
	_, err := s.CreateVolume(context.Background(), &csipb.CreateVolumeRequest{Name: "x", VolumeCapabilities: []*csipb.VolumeCapability{rwxCapability()}, Parameters: map[string]string{BackingStorageClassParameter: "fast", BackingDirectoryParameter: "../escape"}})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("code=%s err=%v", status.Code(err), err)
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
