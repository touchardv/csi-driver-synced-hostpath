package synced

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	grpc "google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"k8s.io/klog/v2"
)

// streamReader wraps the gRPC Recv() method to satisfy io.Reader
type streamReader struct {
	stream grpc.ServerStreamingClient[DownloadResponse]
	buf    []byte
}

func (r *streamReader) Read(p []byte) (n int, err error) {
	if len(r.buf) == 0 {
		resp, err := r.stream.Recv()
		if err != nil {
			return 0, err // Returns io.EOF when stream ends
		}
		r.buf = resp.Chunk
	}
	n = copy(p, r.buf)
	r.buf = r.buf[n:]
	return n, nil
}

func ClientDownload(ctx context.Context, addr string, volumeID string, destDir string) error {
	klog.V(4).Info("Downloading volume archive")
	conn, _ := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	defer conn.Close()
	client := NewFileServiceClient(conn)
	downStream, err := client.Download(ctx, &DownloadRequest{VolumeID: volumeID})
	if err != nil {
		klog.Warningf("Failed to download volume archive: %v", err)
		return err
	}
	reader := &streamReader{stream: downStream}
	tr := tar.NewReader(reader)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break // End of archive
		}
		if err != nil {
			return err
		}

		// Determine the target path safely
		target := filepath.Join(destDir, header.Name)

		switch header.Typeflag {
		case tar.TypeDir:
			klog.V(4).Infof("Creating directory %s", target)
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
		case tar.TypeReg:
			// Ensure parent directory exists
			klog.V(4).Infof("Creating parent directory %s", filepath.Dir(target))
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}

			// Create the file
			klog.V(4).Infof("Creating file %s", target)
			f, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR, os.FileMode(header.Mode))
			if err != nil {
				return err
			}

			// Stream the content from the tar reader to the file
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return err
			}
			f.Close()
		}
	}
	return nil
}

func ClientUpload(addr string, volumeDir string, volumeID string) error {
	klog.V(4).Info("Uploading volume archive")
	conn, _ := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	defer conn.Close()
	client := NewFileServiceClient(conn)

	reader, writer := io.Pipe()
	go func() {
		err := createArchive(volumeDir, writer)
		writer.CloseWithError(err)
	}()

	stream, err := client.Upload(context.Background())
	if err != nil {
		return fmt.Errorf("could not open stream: %v", err)
	}

	// 4. Send the metadata (Volume ID) first
	err = stream.Send(&UploadRequest{
		Data: &UploadRequest_VolumeID{VolumeID: volumeID},
	})
	if err != nil {
		klog.Infof("Failed to send volume ID %s", volumeID)
		return err
	}

	buf := make([]byte, 64*1024) // 64KB chunks
	for {
		n, err := reader.Read(buf)
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		err = stream.Send(&UploadRequest{
			Data: &UploadRequest_Chunk{Chunk: buf[:n]},
		})
		if err != nil {
			return err
		}
	}

	res, err := stream.CloseAndRecv()
	if err != nil {
		return err
	}

	klog.Infof("Upload success: %s (%d bytes)", res.Message, res.SizeBytes)
	return nil
}
