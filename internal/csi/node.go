package csi

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	csipb "github.com/container-storage-interface/spec/lib/go/csi"
	storagev1alpha1 "github.com/rwox-csi/rwox-csi/api/v1alpha1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/mount-utils"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type NodeServer struct {
	csipb.UnimplementedNodeServer
	Client  client.Client
	NodeID  string
	Mounter mount.Interface
}

func (s *NodeServer) mounter() mount.Interface {
	if s.Mounter != nil {
		return s.Mounter
	}
	return mount.New("")
}
func (s *NodeServer) NodeStageVolume(ctx context.Context, req *csipb.NodeStageVolumeRequest) (*csipb.NodeStageVolumeResponse, error) {
	if req.VolumeId == "" || req.StagingTargetPath == "" {
		return nil, status.Error(codes.InvalidArgument, "volume_id and staging_target_path are required")
	}
	if err := validateCapabilities([]*csipb.VolumeCapability{req.VolumeCapability}); err != nil {
		return nil, err
	}
	v := &storagev1alpha1.RWXVolume{}
	if err := s.Client.Get(ctx, types.NamespacedName{Name: req.VolumeId}, v); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, status.Error(codes.NotFound, "volume not found")
		}
		return nil, status.Errorf(codes.Unavailable, "resolve volume: %v", err)
	}
	if !v.Ready() {
		return nil, status.Error(codes.FailedPrecondition, "volume is not ready")
	}
	if err := os.MkdirAll(req.StagingTargetPath, 0750); err != nil {
		return nil, status.Errorf(codes.Internal, "create staging path: %v", err)
	}
	notMounted, err := s.mounter().IsLikelyNotMountPoint(req.StagingTargetPath)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "inspect staging path: %v", err)
	}
	if !notMounted {
		return &csipb.NodeStageVolumeResponse{}, nil
	}
	opts := []string{}
	hasVersion := false
	for _, o := range req.VolumeCapability.GetMount().MountFlags {
		if o != "ro" {
			opts = append(opts, o)
		}
		if strings.HasPrefix(o, "vers=") || strings.HasPrefix(o, "nfsvers=") {
			hasVersion = true
		}
	}
	if !hasVersion {
		opts = append(opts, "nfsvers=4.1")
	}
	exportPath := v.Spec.NFS.ExportPath
	if exportPath == "" {
		exportPath = "/"
	}
	source := v.Status.Gateway.DNSName + ":" + exportPath
	if err := s.mounter().Mount(source, req.StagingTargetPath, "nfs4", opts); err != nil {
		return nil, status.Errorf(codes.Internal, "mount NFS export: %v", err)
	}
	return &csipb.NodeStageVolumeResponse{}, nil
}
func (s *NodeServer) NodeUnstageVolume(ctx context.Context, req *csipb.NodeUnstageVolumeRequest) (*csipb.NodeUnstageVolumeResponse, error) {
	if req.StagingTargetPath == "" {
		return nil, status.Error(codes.InvalidArgument, "staging_target_path is required")
	}
	if err := mount.CleanupMountPoint(req.StagingTargetPath, s.mounter(), true); err != nil {
		return nil, status.Errorf(codes.Internal, "unmount staging path: %v", err)
	}
	return &csipb.NodeUnstageVolumeResponse{}, nil
}
func (s *NodeServer) NodePublishVolume(ctx context.Context, req *csipb.NodePublishVolumeRequest) (*csipb.NodePublishVolumeResponse, error) {
	if req.StagingTargetPath == "" || req.TargetPath == "" {
		return nil, status.Error(codes.InvalidArgument, "staging_target_path and target_path are required")
	}
	if err := os.MkdirAll(req.TargetPath, 0750); err != nil {
		return nil, status.Errorf(codes.Internal, "create target path: %v", err)
	}
	notMounted, err := s.mounter().IsLikelyNotMountPoint(req.TargetPath)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "inspect target path: %v", err)
	}
	if !notMounted {
		return &csipb.NodePublishVolumeResponse{}, nil
	}
	if err := s.mounter().Mount(req.StagingTargetPath, req.TargetPath, "", []string{"bind"}); err != nil {
		return nil, status.Errorf(codes.Internal, "bind mount: %v", err)
	}
	if req.Readonly {
		if err := s.mounter().Mount(req.StagingTargetPath, req.TargetPath, "", []string{"bind", "remount", "ro"}); err != nil {
			_ = s.mounter().Unmount(req.TargetPath)
			return nil, status.Errorf(codes.Internal, "make bind mount read-only: %v", err)
		}
	}
	return &csipb.NodePublishVolumeResponse{}, nil
}
func (s *NodeServer) NodeUnpublishVolume(ctx context.Context, req *csipb.NodeUnpublishVolumeRequest) (*csipb.NodeUnpublishVolumeResponse, error) {
	if req.TargetPath == "" {
		return nil, status.Error(codes.InvalidArgument, "target_path is required")
	}
	if err := mount.CleanupMountPoint(req.TargetPath, s.mounter(), true); err != nil {
		return nil, status.Errorf(codes.Internal, "unmount target: %v", err)
	}
	return &csipb.NodeUnpublishVolumeResponse{}, nil
}
func (s *NodeServer) NodeGetVolumeStats(ctx context.Context, req *csipb.NodeGetVolumeStatsRequest) (*csipb.NodeGetVolumeStatsResponse, error) {
	if req.VolumePath == "" {
		return nil, status.Error(codes.InvalidArgument, "volume_path is required")
	}
	if _, err := os.Stat(filepath.Clean(req.VolumePath)); err != nil {
		if os.IsNotExist(err) {
			return nil, status.Error(codes.NotFound, "volume path does not exist")
		}
		return nil, status.Errorf(codes.Internal, "stat volume path: %v", err)
	}
	var st syscall.Statfs_t
	if err := syscall.Statfs(req.VolumePath, &st); err != nil {
		return nil, status.Errorf(codes.Internal, "statfs: %v", err)
	}
	total := int64(st.Blocks) * int64(st.Bsize)
	avail := int64(st.Bavail) * int64(st.Bsize)
	files := int64(st.Files)
	free := int64(st.Ffree)
	return &csipb.NodeGetVolumeStatsResponse{Usage: []*csipb.VolumeUsage{{Unit: csipb.VolumeUsage_BYTES, Total: total, Available: avail, Used: total - avail}, {Unit: csipb.VolumeUsage_INODES, Total: files, Available: free, Used: files - free}}}, nil
}
func (s *NodeServer) NodeGetInfo(context.Context, *csipb.NodeGetInfoRequest) (*csipb.NodeGetInfoResponse, error) {
	if s.NodeID == "" {
		return nil, status.Error(codes.Internal, "node ID is not configured")
	}
	return &csipb.NodeGetInfoResponse{NodeId: s.NodeID}, nil
}
func (s *NodeServer) NodeGetCapabilities(context.Context, *csipb.NodeGetCapabilitiesRequest) (*csipb.NodeGetCapabilitiesResponse, error) {
	rpc := func(t csipb.NodeServiceCapability_RPC_Type) *csipb.NodeServiceCapability {
		return &csipb.NodeServiceCapability{Type: &csipb.NodeServiceCapability_Rpc{Rpc: &csipb.NodeServiceCapability_RPC{Type: t}}}
	}
	return &csipb.NodeGetCapabilitiesResponse{Capabilities: []*csipb.NodeServiceCapability{rpc(csipb.NodeServiceCapability_RPC_STAGE_UNSTAGE_VOLUME), rpc(csipb.NodeServiceCapability_RPC_GET_VOLUME_STATS)}}, nil
}
