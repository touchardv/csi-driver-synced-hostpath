package driver

import (
	"context"
	"net"
	"os"
	"path/filepath"

	csi "github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/touchardv/csi-driver-synced-hostpath/internal/synced"
	"google.golang.org/grpc"
	"k8s.io/klog/v2"
)

const (
	DriverName    = "syncedhostpath.csi.k8s.io"
	VendorVersion = "v0.0.2"
)

// SyncedHostPathDriver implements the three required CSI services.
type SyncedHostPathDriver struct {
	csi.UnimplementedIdentityServer
	csi.UnimplementedControllerServer
	csi.UnimplementedNodeServer

	nodeID     string
	svc        synced.Service
	csiServer  *grpc.Server
	fileServer synced.FileServer
}

func NewSyncedHostPathDriver(nodeID string, stateDir string, enableFileServer bool, fileServerAddr string) *SyncedHostPathDriver {
	var fileServer synced.FileServer
	if enableFileServer {
		fileServer = synced.NewFileServer(filepath.Join(stateDir, "uploads"))
	}
	return &SyncedHostPathDriver{
		nodeID:     nodeID,
		svc:        synced.NewService(stateDir, fileServerAddr),
		csiServer:  grpc.NewServer(),
		fileServer: fileServer,
	}
}

func (n *SyncedHostPathDriver) Run(ctx context.Context, socketPath string) {
	klog.Info("Running driver")
	if n.fileServer != nil {
		if err := n.fileServer.Run(n.svc); err != nil {
			klog.Fatalf("failed to start file server: %v", err)
		}
	}

	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		klog.Fatalf("failed to remove %s: %v", socketPath, err)
	}

	csiListener, err := net.Listen("unix", socketPath)
	if err != nil {
		klog.Fatalf("failed to listen on socket %s: %v", socketPath, err)
	}
	klog.Infof("Listening on %s", csiListener.Addr().String())

	csi.RegisterIdentityServer(n.csiServer, n)
	csi.RegisterControllerServer(n.csiServer, n)
	csi.RegisterNodeServer(n.csiServer, n)

	go func() {
		err := n.csiServer.Serve(csiListener)
		if err != nil {
			klog.Fatalf("failed to serve: %v", err)
		}
	}()
}

func (n *SyncedHostPathDriver) Stop() {
	klog.Info("Stopping driver")
	n.csiServer.GracefulStop()
	if n.fileServer != nil {
		n.fileServer.Stop()
	}
	n.svc.Stop()
}
