package csi

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"time"

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

const (
	BackingStorageClassParameter    = "backingStorageClass"
	DirectoryModeParameter          = "directoryMode"
	BackingDirectoryParameter       = "backingDirectory"
	defaultProvisioningPollInterval = 250 * time.Millisecond
)

type ControllerServer struct {
	csipb.UnimplementedControllerServer
	Client                   client.Client
	ProvisioningPollInterval time.Duration
}

func (s *ControllerServer) provisioningPollInterval() time.Duration {
	if s.ProvisioningPollInterval > 0 {
		return s.ProvisioningPollInterval
	}
	return defaultProvisioningPollInterval
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
	directoryMode := req.Parameters[DirectoryModeParameter]
	if !storagev1alpha1.ValidDirectoryMode(directoryMode) {
		return nil, status.Errorf(codes.InvalidArgument, "StorageClass parameter %q must be an octal mode such as 0777", DirectoryModeParameter)
	}
	backingDirectory := req.Parameters[BackingDirectoryParameter]
	if !storagev1alpha1.ValidBackingDirectory(backingDirectory) {
		return nil, status.Errorf(codes.InvalidArgument, "StorageClass parameter %q must be a single relative directory name", BackingDirectoryParameter)
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
		v = &storagev1alpha1.RWXVolume{TypeMeta: metav1.TypeMeta{APIVersion: storagev1alpha1.GroupVersion.String(), Kind: "RWXVolume"}, ObjectMeta: metav1.ObjectMeta{Name: name, Annotations: map[string]string{"storage.example.io/csi-name": req.Name}}, Spec: storagev1alpha1.RWXVolumeSpec{Capacity: want, Backing: storagev1alpha1.BackingSpec{StorageClassName: backing}, NFS: storagev1alpha1.NFSSpec{Version: "4.1", ExportPath: "/", DirectoryMode: directoryMode, BackingDirectory: backingDirectory}}}
		if err := s.Client.Create(ctx, v); err != nil && !apierrors.IsAlreadyExists(err) {
			return nil, status.Errorf(codes.Internal, "create RWXVolume: %v", err)
		}
	} else if err != nil {
		return nil, status.Errorf(codes.Internal, "get RWXVolume: %v", err)
	}
	return s.waitForVolume(ctx, name, backing, directoryMode, backingDirectory, want)
}

func (s *ControllerServer) waitForVolume(ctx context.Context, name, backing, directoryMode, backingDirectory string, want resource.Quantity) (*csipb.CreateVolumeResponse, error) {
	ticker := time.NewTicker(s.provisioningPollInterval())
	defer ticker.Stop()
	for {
		v := &storagev1alpha1.RWXVolume{}
		if err := s.Client.Get(ctx, types.NamespacedName{Name: name}, v); err != nil {
			if ctx.Err() != nil {
				return nil, status.FromContextError(ctx.Err()).Err()
			}
			return nil, status.Errorf(codes.Internal, "get RWXVolume: %v", err)
		}
		if v.Spec.Backing.StorageClassName != backing || v.Spec.NFS.DirectoryMode != directoryMode || v.Spec.NFS.BackingDirectory != backingDirectory || v.Spec.Capacity.Cmp(want) < 0 {
			return nil, status.Error(codes.AlreadyExists, "a volume with this name exists with incompatible parameters")
		}
		if v.Status.Failure != nil && v.Status.Failure.Class == "Permanent" {
			return nil, status.Errorf(codes.FailedPrecondition, "%s: %s", v.Status.Failure.Reason, v.Status.Failure.Message)
		}
		if v.Ready() {
			cap := v.Spec.Capacity.Value()
			if v.Status.Capacity != nil {
				cap = v.Status.Capacity.Value()
			}
			return &csipb.CreateVolumeResponse{Volume: &csipb.Volume{VolumeId: name, CapacityBytes: cap, VolumeContext: map[string]string{"protocol": "nfs4.1", "export": "/"}}}, nil
		}
		select {
		case <-ctx.Done():
			return nil, status.FromContextError(ctx.Err()).Err()
		case <-ticker.C:
		}
	}
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
