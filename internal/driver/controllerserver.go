package driver

import (
	"context"
	"fmt"

	csi "github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/klog/v2"
)

func (n *SyncedHostPathDriver) ControllerGetCapabilities(ctx context.Context, req *csi.ControllerGetCapabilitiesRequest) (*csi.ControllerGetCapabilitiesResponse, error) {
	klog.V(2).Info("Controller: GetCapabilities called")
	return &csi.ControllerGetCapabilitiesResponse{
		Capabilities: []*csi.ControllerServiceCapability{
			{
				Type: &csi.ControllerServiceCapability_Rpc{
					Rpc: &csi.ControllerServiceCapability_RPC{
						Type: csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME,
					},
				},
			},
		},
	}, nil
}

func (n *SyncedHostPathDriver) CreateVolume(ctx context.Context, req *csi.CreateVolumeRequest) (*csi.CreateVolumeResponse, error) {
	klog.V(2).Info("Controller: CreateVolume called")
	if req.GetVolumeContentSource() != nil {
		return nil, status.Errorf(codes.InvalidArgument, "unsupported volume content source")
	}
	if len(req.GetMutableParameters()) > 0 {
		return nil, status.Errorf(codes.InvalidArgument, "unsupported mutable parameters")
	}
	name := req.GetName()
	if len(name) == 0 {
		return nil, status.Error(codes.InvalidArgument, "missing volume name")
	}
	caps := req.GetVolumeCapabilities()
	if caps == nil {
		return nil, status.Error(codes.InvalidArgument, "missing volume capabilities")
	}
	for _, cap := range caps {
		if cap.GetBlock() != nil {
			return nil, status.Error(codes.InvalidArgument, "cannot use block access type")
		}
	}
	capacity := int64(req.GetCapacityRange().GetRequiredBytes())
	limit := int64(req.GetCapacityRange().GetLimitBytes())
	if capacity < 0 || limit < 0 {
		return nil, status.Error(codes.InvalidArgument, "cannot have negative capacity")
	}
	if limit > 0 && capacity > limit {
		return nil, status.Error(codes.InvalidArgument, "cannot have capacity exceeding limit")
	}
	if capacity == 0 && limit > 0 {
		capacity = limit
	}

	if v, found := n.svc.GetVolumeByName(name); found {
		volumeCapacity := v.Capacity
		if volumeCapacity < capacity || (limit > 0 && volumeCapacity > limit) {
			return nil, status.Error(codes.AlreadyExists, "cannot update existing volume size")
		}
		return &csi.CreateVolumeResponse{
			Volume: &csi.Volume{
				CapacityBytes: volumeCapacity,
				VolumeId:      v.ID,
				VolumeContext: req.GetParameters(),
			},
		}, nil
	}

	volumeID, err := n.svc.CreateVolume(name, capacity)
	if err != nil {
		return nil, err
	}
	klog.Infof("Created volume name=%s with id=%s", name, volumeID)

	return &csi.CreateVolumeResponse{
		Volume: &csi.Volume{
			CapacityBytes: capacity,
			VolumeId:      volumeID,
			VolumeContext: req.GetParameters(),
		},
	}, nil
}

func (n *SyncedHostPathDriver) DeleteVolume(_ context.Context, req *csi.DeleteVolumeRequest) (*csi.DeleteVolumeResponse, error) {
	klog.V(2).Info("Controller: DeleteVolume called")
	volumeID := req.GetVolumeId()
	if len(volumeID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "missing volume id")
	}
	if _, found := n.svc.GetVolumeByID(volumeID); !found {
		klog.Warningf("Deleted volume id=%s not found", volumeID)
		return &csi.DeleteVolumeResponse{}, nil
	}
	if err := n.svc.DeleteVolume(volumeID); err != nil {
		return nil, fmt.Errorf("failed to delete volume: %w", err)
	}
	klog.Infof("Deleted volume id=%s", volumeID)

	return &csi.DeleteVolumeResponse{}, nil
}

// ValidateVolumeCapabilities implements csi.ControllerServer.
func (n *SyncedHostPathDriver) ValidateVolumeCapabilities(ctx context.Context, req *csi.ValidateVolumeCapabilitiesRequest) (*csi.ValidateVolumeCapabilitiesResponse, error) {
	klog.V(2).Info("Controller: ValidateVolumeCapabilities called")
	volumeID := req.GetVolumeId()
	if len(volumeID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "missing volume id")
	}
	volumeCapabilities := req.GetVolumeCapabilities()
	if volumeCapabilities == nil {
		return nil, status.Error(codes.InvalidArgument, "missing volume capabilities")
	}
	if len(volumeCapabilities) == 0 {
		return nil, status.Error(codes.InvalidArgument, "missing volume capabilities")
	}

	_, found := n.svc.GetVolumeByID(volumeID)
	if !found {
		return nil, status.Error(codes.NotFound, "volume not found")
	}

	for _, cap := range volumeCapabilities {
		if cap.GetAccessMode().GetMode() != csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER {
			return &csi.ValidateVolumeCapabilitiesResponse{
				Message: "Only Single Node Writer (ReadWriteOnce) is supported",
			}, nil
		}
		if cap.GetBlock() != nil {
			return &csi.ValidateVolumeCapabilitiesResponse{
				Message: "Block access type is not supported",
			}, nil
		}
	}

	return &csi.ValidateVolumeCapabilitiesResponse{
		Confirmed: &csi.ValidateVolumeCapabilitiesResponse_Confirmed{
			VolumeContext:      req.GetVolumeContext(),
			VolumeCapabilities: volumeCapabilities,
			Parameters:         req.GetParameters(),
		},
	}, nil
}
