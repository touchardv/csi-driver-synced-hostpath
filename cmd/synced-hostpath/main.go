package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/touchardv/csi-driver-synced-hostpath/internal/driver"
	"k8s.io/klog/v2"
)

var (
	socketPath       = flag.String("socket-path", "/tmp/csi.sock", "CSI unix socket path")
	stateDir         = flag.String("state-dir", "", "CSI driver state directory")
	nodeID           = flag.String("nodeid", "", "node id")
	enableFileServer = flag.Bool("enable-file-server", false, "flag controlling the file server")
	fileServerAddr   = flag.String("file-server-addr", "localhost:50051", "hostname with port (or address) of the file server")
)

func main() {
	klog.InitFlags(nil)
	defer klog.Flush()

	ctx, stopFunc := context.WithCancel(context.Background())
	flag.Parse()
	driver := driver.NewSyncedHostPathDriver(*nodeID, *stateDir, *enableFileServer, *fileServerAddr)
	driver.Run(ctx, *socketPath)

	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
	<-c

	stopFunc()
	driver.Stop()
	os.Exit(0)
}
