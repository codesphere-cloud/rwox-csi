package csi

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"

	csipb "github.com/container-storage-interface/spec/lib/go/csi"
	storagev1alpha1 "github.com/rwox-csi/rwox-csi/api/v1alpha1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const BackingStorageClassParameter = "backingStorageClass"

type ControllerServer struct {
	csipb.UnimplementedControllerServer
	Client client.Client
}

func volumeName(name string) string {
	sum := sha256.Sum256([]byte(name))
	return fmt.Sprintf("rwxvol-%x", sum[:10])
}
func requestedBytes(r *csipb.CapacityRange) int64 {
	if r == nil || r.RequiredBytes <= 0 {
		return 1 << 30
	}
	return r.RequiredBytes
}

func (s *ControllerServer) CreateVolume(ctx context.Context, req *csipb.CreateVolumeRequest) (*csipb.CreateVolumeResponse, error) {
	if strings.TrimSpace(req.Name) == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}
	if err := validateCapabilities(req.VolumeCapabilities); err != nil {
		return nil, err
	}
	if req.VolumeContentSource != nil {
		return nil, status.Error(codes.InvalidArgument, "snapshots and clones are not supported")
	}
	backing := req.Parameters[BackingStorageClassParameter]
	if backing == "" {
		return nil, status.Errorf(codes.InvalidArgument, "StorageClass parameter %q is required", BackingStorageClassParameter)
	}
	name := volumeName(req.Name)
	bytes := requestedBytes(req.CapacityRange)
	if req.CapacityRange != nil && req.CapacityRange.LimitBytes > 0 && bytes > req.CapacityRange.LimitBytes {
		return nil, status.Error(codes.OutOfRange, "required capacity exceeds limit")
	}
	want := *resource.NewQuantity(bytes, resource.BinarySI)
	v := &storagev1alpha1.RWXVolume{}
	err := s.Client.Get(ctx, types.NamespacedName{Name: name}, v)
	if apierrors.IsNotFound(err) {
		v = &storagev1alpha1.RWXVolume{TypeMeta: metav1.TypeMeta{APIVersion: storagev1alpha1.GroupVersion.String(), Kind: "RWXVolume"}, ObjectMeta: metav1.ObjectMeta{Name: name, Annotations: map[string]string{"storage.example.io/csi-name": req.Name}}, Spec: storagev1alpha1.RWXVolumeSpec{Capacity: want, Backing: storagev1alpha1.BackingSpec{StorageClassName: backing}, NFS: storagev1alpha1.NFSSpec{Version: "4.1", ExportPath: "/"}}}
		if err := s.Client.Create(ctx, v); err != nil {
			return nil, status.Errorf(codes.Internal, "create RWXVolume: %v", err)
		}
		return nil, status.Error(codes.Aborted, "volume provisioning started")
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "get RWXVolume: %v", err)
	}
	if v.Spec.Backing.StorageClassName != backing || v.Spec.Capacity.Cmp(want) < 0 {
		return nil, status.Error(codes.AlreadyExists, "a volume with this name exists with incompatible parameters")
	}
	if v.Status.Failure != nil && v.Status.Failure.Class == "Permanent" {
		return nil, status.Errorf(codes.FailedPrecondition, "%s: %s", v.Status.Failure.Reason, v.Status.Failure.Message)
	}
	if !v.Ready() {
		return nil, status.Error(codes.Aborted, "volume is still provisioning")
	}
	cap := v.Spec.Capacity.Value()
	if v.Status.Capacity != nil {
		cap = v.Status.Capacity.Value()
	}
	return &csipb.CreateVolumeResponse{Volume: &csipb.Volume{VolumeId: name, CapacityBytes: cap, VolumeContext: map[string]string{"protocol": "nfs4.1", "export": "/"}}}, nil
}
func (s *ControllerServer) DeleteVolume(ctx context.Context, req *csipb.DeleteVolumeRequest) (*csipb.DeleteVolumeResponse, error) {
	if req.VolumeId == "" {
		return &csipb.DeleteVolumeResponse{}, nil
	}
	v := &storagev1alpha1.RWXVolume{ObjectMeta: metav1.ObjectMeta{Name: req.VolumeId}}
	if err := s.Client.Delete(ctx, v); err != nil && !apierrors.IsNotFound(err) {
		return nil, status.Errorf(codes.Internal, "delete RWXVolume: %v", err)
	}
	return &csipb.DeleteVolumeResponse{}, nil
}
func (s *ControllerServer) ControllerExpandVolume(ctx context.Context, req *csipb.ControllerExpandVolumeRequest) (*csipb.ControllerExpandVolumeResponse, error) {
	if req.VolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, "volume_id is required")
	}
	want := *resource.NewQuantity(requestedBytes(req.CapacityRange), resource.BinarySI)
	v := &storagev1alpha1.RWXVolume{}
	if err := s.Client.Get(ctx, types.NamespacedName{Name: req.VolumeId}, v); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, status.Error(codes.NotFound, "volume not found")
		}
		return nil, status.Errorf(codes.Internal, "get volume: %v", err)
	}
	if want.Cmp(v.Spec.Capacity) > 0 {
		base := v.DeepCopy()
		v.Spec.Capacity = want
		if err := s.Client.Patch(ctx, v, client.MergeFrom(base)); err != nil {
			return nil, status.Errorf(codes.Internal, "request expansion: %v", err)
		}
		return nil, status.Error(codes.Aborted, "volume expansion started")
	}
	if v.Status.Capacity == nil || v.Status.Capacity.Cmp(want) < 0 {
		return nil, status.Error(codes.Aborted, "volume expansion is still in progress")
	}
	return &csipb.ControllerExpandVolumeResponse{CapacityBytes: v.Status.Capacity.Value(), NodeExpansionRequired: false}, nil
}
func (s *ControllerServer) ValidateVolumeCapabilities(ctx context.Context, req *csipb.ValidateVolumeCapabilitiesRequest) (*csipb.ValidateVolumeCapabilitiesResponse, error) {
	if req.VolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, "volume_id is required")
	}
	v := &storagev1alpha1.RWXVolume{}
	if err := s.Client.Get(ctx, types.NamespacedName{Name: req.VolumeId}, v); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, status.Error(codes.NotFound, "volume not found")
		}
		return nil, status.Errorf(codes.Internal, "get volume: %v", err)
	}
	if err := validateCapabilities(req.VolumeCapabilities); err != nil {
		return &csipb.ValidateVolumeCapabilitiesResponse{Message: err.Error()}, nil
	}
	return &csipb.ValidateVolumeCapabilitiesResponse{Confirmed: &csipb.ValidateVolumeCapabilitiesResponse_Confirmed{VolumeCapabilities: req.VolumeCapabilities}}, nil
}
func (s *ControllerServer) ControllerGetCapabilities(context.Context, *csipb.ControllerGetCapabilitiesRequest) (*csipb.ControllerGetCapabilitiesResponse, error) {
	types := []csipb.ControllerServiceCapability_RPC_Type{csipb.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME, csipb.ControllerServiceCapability_RPC_EXPAND_VOLUME}
	out := &csipb.ControllerGetCapabilitiesResponse{}
	for _, t := range types {
		out.Capabilities = append(out.Capabilities, &csipb.ControllerServiceCapability{Type: &csipb.ControllerServiceCapability_Rpc{Rpc: &csipb.ControllerServiceCapability_RPC{Type: t}}})
	}
	return out, nil
}
