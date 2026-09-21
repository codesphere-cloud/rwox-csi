package csi

import (
	csipb "github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func validateCapabilities(caps []*csipb.VolumeCapability) error {
	if len(caps) == 0 {
		return status.Error(codes.InvalidArgument, "volume capabilities are required")
	}
	for _, cap := range caps {
		if cap.GetBlock() != nil {
			return status.Error(codes.InvalidArgument, "raw block volumes are not supported")
		}
		if cap.GetMount() == nil {
			return status.Error(codes.InvalidArgument, "filesystem mount capability is required")
		}
		if cap.GetAccessMode() == nil || cap.GetAccessMode().GetMode() != csipb.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER {
			return status.Error(codes.InvalidArgument, "only MULTI_NODE_MULTI_WRITER is supported")
		}
	}
	return nil
}
