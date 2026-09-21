package csi

import (
	"context"

	csipb "github.com/container-storage-interface/spec/lib/go/csi"
)

type IdentityServer struct {
	csipb.UnimplementedIdentityServer
	Name, Version string
}

func (s *IdentityServer) GetPluginInfo(context.Context, *csipb.GetPluginInfoRequest) (*csipb.GetPluginInfoResponse, error) {
	return &csipb.GetPluginInfoResponse{Name: s.Name, VendorVersion: s.Version}, nil
}
func (s *IdentityServer) GetPluginCapabilities(context.Context, *csipb.GetPluginCapabilitiesRequest) (*csipb.GetPluginCapabilitiesResponse, error) {
	return &csipb.GetPluginCapabilitiesResponse{Capabilities: []*csipb.PluginCapability{{Type: &csipb.PluginCapability_Service_{Service: &csipb.PluginCapability_Service{Type: csipb.PluginCapability_Service_CONTROLLER_SERVICE}}}, {Type: &csipb.PluginCapability_VolumeExpansion_{VolumeExpansion: &csipb.PluginCapability_VolumeExpansion{Type: csipb.PluginCapability_VolumeExpansion_ONLINE}}}}}, nil
}
func (s *IdentityServer) Probe(context.Context, *csipb.ProbeRequest) (*csipb.ProbeResponse, error) {
	return &csipb.ProbeResponse{}, nil
}
