package driver

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"

	csi "github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/touchardv/csi-driver-synced-hostpath/internal/synced"
	"google.golang.org/grpc"
	"k8s.io/klog/v2"
)

const (
	DriverName = "syncedhostpath.csi.k8s.io"
)

var (
	VendorVersion = "v0.0.1"
	BuildTime     = "unknown"
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

func (n *SyncedHostPathDriver) Run(ctx context.Context, socketPath string) error {
	klog.Infof("Running driver version %s (built %s)", VendorVersion, BuildTime)
	if n.fileServer != nil {
		if err := n.fileServer.Run(n.svc); err != nil {
			return fmt.Errorf("failed to start file server: %w", err)
		}
	}

	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove %s: %w", socketPath, err)
	}

	csiListener, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("failed to listen on socket %s: %w", socketPath, err)
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
	return nil
}

func (n *SyncedHostPathDriver) Stop() {
	klog.Info("Stopping driver")
	n.csiServer.GracefulStop()
	if n.fileServer != nil {
		n.fileServer.Stop()
	}
	n.svc.Stop()
}
