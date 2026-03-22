package driver

import (
	"context"

	csi "github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/klog/v2"
)

func (n *SyncedHostPathDriver) NodeGetCapabilities(_ context.Context, req *csi.NodeGetCapabilitiesRequest) (*csi.NodeGetCapabilitiesResponse, error) {
	klog.V(2).Info("Node: GetCapabilities called")
	caps := []*csi.NodeServiceCapability{
		{
			Type: &csi.NodeServiceCapability_Rpc{
				Rpc: &csi.NodeServiceCapability_RPC{
					Type: csi.NodeServiceCapability_RPC_STAGE_UNSTAGE_VOLUME,
				},
			},
		},
		{
			Type: &csi.NodeServiceCapability_Rpc{
				Rpc: &csi.NodeServiceCapability_RPC{
					Type: csi.NodeServiceCapability_RPC_SINGLE_NODE_MULTI_WRITER,
				},
			},
		},
	}

	return &csi.NodeGetCapabilitiesResponse{Capabilities: caps}, nil
}

func (n *SyncedHostPathDriver) NodeGetInfo(_ context.Context, req *csi.NodeGetInfoRequest) (*csi.NodeGetInfoResponse, error) {
	klog.V(2).Info("Node: GetInfo called")
	return &csi.NodeGetInfoResponse{
		NodeId: n.nodeID,
	}, nil
}

func (n *SyncedHostPathDriver) NodeStageVolume(ctx context.Context, req *csi.NodeStageVolumeRequest) (*csi.NodeStageVolumeResponse, error) {
	klog.V(2).Info("Node: StageVolume called")
	volumeID := req.GetVolumeId()
	if len(volumeID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "missing volume id")
	}
	stagingTargetPath := req.GetStagingTargetPath()
	if len(stagingTargetPath) == 0 {
		return nil, status.Error(codes.InvalidArgument, "missing staging target path")
	}
	volumeCapability := req.GetVolumeCapability()
	if volumeCapability == nil {
		return nil, status.Error(codes.InvalidArgument, "missing volume capabilities")
	}

	if v, found := n.svc.GetVolumeByID(volumeID); found {
		if v.Staged {
			if v.StagedPath == stagingTargetPath {
				// idempotency
				return &csi.NodeStageVolumeResponse{}, nil
			}
			return nil, status.Error(codes.InvalidArgument, "volume already staged to a different path")
		}
	}

	err := n.svc.StageVolume(ctx, volumeID, stagingTargetPath)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "stage volume failed: %v", err)
	}
	klog.Infof("Staged volume id=%s to: %s", volumeID, stagingTargetPath)

	return &csi.NodeStageVolumeResponse{}, nil
}

func (n *SyncedHostPathDriver) NodePublishVolume(ctx context.Context, req *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {
	klog.V(2).Info("Node: PublishVolume called")
	volumeID := req.GetVolumeId()
	if len(volumeID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "missing volume id")
	}
	stagingTargetPath := req.GetStagingTargetPath()
	if len(stagingTargetPath) == 0 {
		return nil, status.Error(codes.InvalidArgument, "missing staging target path")
	}
	targetPath := req.GetTargetPath()
	if len(targetPath) == 0 {
		return nil, status.Error(codes.InvalidArgument, "missing target path")
	}
	volumeCapability := req.GetVolumeCapability()
	if volumeCapability == nil {
		return nil, status.Error(codes.InvalidArgument, "missing volume capabilities")
	}
	if volumeCapability.GetBlock() != nil {
		return nil, status.Error(codes.InvalidArgument, "cannot use block access type")
	}

	_, found := n.svc.GetVolumeByID(volumeID)
	if !found {
		return nil, status.Error(codes.NotFound, "volume not found")
	}

	readOnly := req.GetReadonly()
	err := n.svc.PublishVolume(volumeID, stagingTargetPath, targetPath, readOnly)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "publish volume failed: %v", err)
	}
	klog.Infof("Volume with id=%s published to %s", volumeID, targetPath)

	return &csi.NodePublishVolumeResponse{}, nil
}

func (n *SyncedHostPathDriver) NodeUnpublishVolume(_ context.Context, req *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error) {
	klog.V(2).Info("Node: UnpublishVolume called")
	volumeID := req.GetVolumeId()
	if len(volumeID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "missing volume id")
	}
	targetPath := req.GetTargetPath()
	if len(targetPath) == 0 {
		return nil, status.Error(codes.InvalidArgument, "missing target path")
	}

	v, found := n.svc.GetVolumeByID(volumeID)
	if !found {
		return nil, status.Error(codes.NotFound, "volume not found")
	}

	if !v.Published {
		return &csi.NodeUnpublishVolumeResponse{}, nil
	}

	err := n.svc.UnpublishVolume(volumeID, targetPath)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "unpublish volume failed: %v", err)
	}
	klog.Infof("Volume with id=%s unpublished from %s", volumeID, targetPath)

	return &csi.NodeUnpublishVolumeResponse{}, nil
}

func (n *SyncedHostPathDriver) NodeUnstageVolume(_ context.Context, req *csi.NodeUnstageVolumeRequest) (*csi.NodeUnstageVolumeResponse, error) {
	klog.V(2).Info("Node: UnstageVolume called")
	volumeID := req.GetVolumeId()
	if len(volumeID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "missing volume id")
	}
	stagingTargetPath := req.GetStagingTargetPath()
	if len(stagingTargetPath) == 0 {
		return nil, status.Error(codes.InvalidArgument, "missing staging target path")
	}

	v, found := n.svc.GetVolumeByID(volumeID)
	if !found {
		return nil, status.Error(codes.NotFound, "volume not found")
	}

	if !v.Staged {
		// idempotency
		return &csi.NodeUnstageVolumeResponse{}, nil
	}

	err := n.svc.UnstageVolume(volumeID, stagingTargetPath)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "unstage volume failed: %v", err)
	}
	klog.Infof("Volume with id=%s unstaged from %s", volumeID, stagingTargetPath)

	return &csi.NodeUnstageVolumeResponse{}, nil
}
