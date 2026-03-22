package synced

import (
	"fmt"
	"io"
	"net"
	"os"

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
		grpcServer: grpc.NewServer(),
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
	klog.V(4).Info("FileServer: Download called")
	path, err := s.handler.Resolve(req.VolumeID)
	if err != nil {
		return err
	}
	file, err := os.Open(path)
	if err != nil {
		klog.Warningf("failed to serve file=%s", path)
		return err
	}
	defer file.Close()

	klog.V(4).Infof("Downloading from file=%s", path)
	buf := make([]byte, 64*1024)
	for {
		n, err := file.Read(buf)
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if err := stream.Send(&DownloadResponse{Chunk: buf[:n]}); err != nil {
			return err
		}
	}
	return nil
}

func (s *fileServer) Upload(stream FileService_UploadServer) error {
	klog.V(4).Info("FileServer: Upload called")

	var file *os.File
	var volumeID string
	var fileSize uint32
	for {
		req, err := stream.Recv()
		if err == io.EOF {
			if file != nil {
				file.Close()
				s.handler.Save(volumeID, file.Name())
			}
			return stream.SendAndClose(&UploadResponse{
				Message:   fmt.Sprintf("Saved %s", volumeID),
				SizeBytes: fileSize,
			})
		}
		if err != nil {
			return err
		}

		switch data := req.Data.(type) {
		case *UploadRequest_VolumeID:
			if volumeID != "" {
				return status.Error(codes.InvalidArgument, "Volume ID already received")
			}
			volumeID = req.GetVolumeID()
			file, err = os.CreateTemp(s.tempDir, "upload-")
			if err != nil {
				return status.Errorf(codes.Internal, "Cannot create temporary file: %v", err)
			}
			defer os.Remove(file.Name())
			klog.V(4).Infof("Uploading to file=%s", file.Name())

		case *UploadRequest_Chunk:
			if file == nil {
				return status.Error(codes.FailedPrecondition, "Volume ID must be sent first")
			}
			n, err := file.Write(data.Chunk)
			if err != nil {
				return status.Errorf(codes.Internal, "Write error: %v", err)
			}
			fileSize += uint32(n)
		}
	}
}
