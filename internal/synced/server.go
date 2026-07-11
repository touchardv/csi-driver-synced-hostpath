package synced

import (
	"fmt"
	"io"
	"net"
	"os"
	"time"

	"google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
	"k8s.io/klog/v2"
)

type FileHandler interface {
	// Resolve the file associated to a given volume, identified by its ID.
	Resolve(volumeID string) (string, error)

	// Save the content of a volume, identified by its ID.
	Save(volumeID string, file string) error
}

type FileServer interface {
	Run(FileHandler) error
	Stop()
}

func NewFileServer(tempDir string) FileServer {
	ensureExistLocalDir(tempDir)
	return &fileServer{
		grpcServer: grpc.NewServer(
			grpc.InitialWindowSize(32*1024*1024),
			grpc.InitialConnWindowSize(32*1024*1024),
			grpc.MaxRecvMsgSize(64*1024*1024),
			grpc.MaxSendMsgSize(64*1024*1024),
		),
		tempDir:    tempDir,
	}
}

type fileServer struct {
	UnimplementedFileServiceServer

	grpcServer *grpc.Server
	handler    FileHandler
	tempDir    string
}

func (s *fileServer) Run(handler FileHandler) error {
	s.handler = handler
	listener, err := net.Listen("tcp", "0.0.0.0:50051")
	if err != nil {
		klog.Fatalf("failed to listen on socket %s: %v", "0.0.0.0:50051", err.Error())
	}
	klog.Infof("Listening on %s", listener.Addr().String())

	RegisterFileServiceServer(s.grpcServer, s)
	go func() {
		if err := s.grpcServer.Serve(listener); err != nil {
			klog.Fatalf("failed to serve: %v", err)
		}
	}()
	return nil
}

func (s *fileServer) Stop() {
	s.grpcServer.GracefulStop()
}

func (s *fileServer) Download(req *DownloadRequest, stream FileService_DownloadServer) error {
	klog.V(2).Infof("FileServer: Download starting for volume %s", req.VolumeID)
	startTime := time.Now()

	path, err := s.handler.Resolve(req.VolumeID)
	if err != nil {
		klog.Warningf("FileServer: Failed to resolve volume %s. Duration: %s, Error: %v", req.VolumeID, time.Since(startTime), err)
		return err
	}
	file, err := os.Open(path)
	if err != nil {
		klog.Warningf("FileServer: Failed to open file=%s for volume %s. Duration: %s, Error: %v", path, req.VolumeID, time.Since(startTime), err)
		return err
	}
	defer file.Close()

	klog.V(4).Infof("Downloading from file=%s", path)
	var bytesSent int64
	buf := make([]byte, 1024*1024)
	for {
		n, err := file.Read(buf)
		if err == io.EOF {
			break
		}
		if err != nil {
			klog.Warningf("FileServer: Failed to read file for volume %s. Duration: %s, Error: %v", req.VolumeID, time.Since(startTime), err)
			return err
		}
		if err := stream.Send(&DownloadResponse{Chunk: buf[:n]}); err != nil {
			klog.Warningf("FileServer: Failed to send chunk for volume %s. Duration: %s, Error: %v", req.VolumeID, time.Since(startTime), err)
			return err
		}
		bytesSent += int64(n)
	}
	duration := time.Since(startTime)
	klog.V(2).Infof("FileServer: Downloaded volume %s successfully. Size: %s, Duration: %s", req.VolumeID, formatBytes(uint64(bytesSent)), duration)
	return nil
}

func (s *fileServer) Upload(stream FileService_UploadServer) error {
	klog.V(2).Info("FileServer: Upload starting")
	startTime := time.Now()

	var file *os.File
	var volumeID string
	var fileSize uint64

	for {
		req, err := stream.Recv()
		if err == io.EOF {
			if file != nil {
				file.Close()
				if err := s.handler.Save(volumeID, file.Name()); err != nil {
					klog.Warningf("FileServer: Failed to save uploaded volume %s. Duration: %s, Error: %v", volumeID, time.Since(startTime), err)
					return err
				}
			}
			duration := time.Since(startTime)
			klog.V(2).Infof("FileServer: Uploaded volume %s successfully. Size: %s, Duration: %s", volumeID, formatBytes(fileSize), duration)
			return stream.SendAndClose(&UploadResponse{
				Message:   fmt.Sprintf("Saved %s", volumeID),
				SizeBytes: fileSize,
			})
		}
		if err != nil {
			klog.Warningf("FileServer: Failed to receive upload stream. Volume: %s, Duration: %s, Error: %v", volumeID, time.Since(startTime), err)
			return err
		}

		switch data := req.Data.(type) {
		case *UploadRequest_VolumeID:
			if volumeID != "" {
				err = status.Error(codes.InvalidArgument, "Volume ID already received")
				klog.Warningf("FileServer: Invalid upload request. Duration: %s, Error: %v", time.Since(startTime), err)
				return err
			}
			volumeID = req.GetVolumeID()

			file, err = os.CreateTemp(s.tempDir, "upload-")
			if err != nil {
				err = status.Errorf(codes.Internal, "Cannot create temporary file: %v", err)
				klog.Warningf("FileServer: Failed to create temp file for volume %s. Duration: %s, Error: %v", volumeID, time.Since(startTime), err)
				return err
			}
			defer os.Remove(file.Name())
			klog.V(4).Infof("Uploading to file=%s", file.Name())

		case *UploadRequest_Chunk:
			if file == nil {
				err = status.Error(codes.FailedPrecondition, "Volume ID must be sent first")
				klog.Warningf("FileServer: Invalid chunk received before volume ID. Duration: %s, Error: %v", time.Since(startTime), err)
				return err
			}
			n, err := file.Write(data.Chunk)
			if err != nil {
				err = status.Errorf(codes.Internal, "Write error: %v", err)
				klog.Warningf("FileServer: Failed to write chunk to temp file for volume %s. Duration: %s, Error: %v", volumeID, time.Since(startTime), err)
				return err
			}
			fileSize += uint64(n)
		}
	}
}
