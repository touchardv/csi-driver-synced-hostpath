package driver

import (
	"context"

	csi "github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/protobuf/types/known/wrapperspb"
	"k8s.io/klog/v2"
)

// GetPluginInfo return the version and name of the plugin
func (n *SyncedHostPathDriver) GetPluginInfo(_ context.Context, _ *csi.GetPluginInfoRequest) (*csi.GetPluginInfoResponse, error) {
	klog.V(2).Info("Identity: GetPluginInfo called")
	return &csi.GetPluginInfoResponse{
		Name:          DriverName,
		VendorVersion: VendorVersion,
	}, nil
}

// GetPluginCapabilities returns the capabilities of the plugin
func (n *SyncedHostPathDriver) GetPluginCapabilities(_ context.Context, _ *csi.GetPluginCapabilitiesRequest) (*csi.GetPluginCapabilitiesResponse, error) {
	klog.V(2).Info("Identity: GetPluginCapabilities called")
	return &csi.GetPluginCapabilitiesResponse{
		Capabilities: []*csi.PluginCapability{
			{
				Type: &csi.PluginCapability_Service_{
					Service: &csi.PluginCapability_Service{
						Type: csi.PluginCapability_Service_CONTROLLER_SERVICE,
					},
				},
			},
		},
	}, nil
}

// Probe returns a response indicating if the plugin is ready.
func (n *SyncedHostPathDriver) Probe(_ context.Context, _ *csi.ProbeRequest) (*csi.ProbeResponse, error) {
	klog.V(4).Info("Identity: Probe called")
	return &csi.ProbeResponse{Ready: &wrapperspb.BoolValue{Value: true}}, nil
}
