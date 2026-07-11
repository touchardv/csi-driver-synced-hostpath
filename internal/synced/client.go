package synced

import (
	"archive/tar"
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	grpc "google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"k8s.io/klog/v2"
)

// streamReader wraps the gRPC Recv() method to satisfy io.Reader
type streamReader struct {
	stream    grpc.ServerStreamingClient[DownloadResponse]
	buf       []byte
	bytesRead int64
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
	r.bytesRead += int64(n)
	return n, nil
}

func ClientDownload(ctx context.Context, addr string, volumeID string, destDir string) error {
	klog.V(2).Infof("Client: Downloading volume %s starting", volumeID)
	startTime := time.Now()
	conn, _ := grpc.NewClient(
		addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithInitialWindowSize(32*1024*1024),
		grpc.WithInitialConnWindowSize(32*1024*1024),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(64*1024*1024),
			grpc.MaxCallSendMsgSize(64*1024*1024),
		),
	)
	defer conn.Close()
	client := NewFileServiceClient(conn)
	downStream, err := client.Download(ctx, &DownloadRequest{VolumeID: volumeID})
	if err != nil {
		klog.Warningf("Client: Failed to download volume %s. Duration: %s, Error: %v", volumeID, time.Since(startTime), err)
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
			klog.Warningf("Client: Failed to read tar header for volume %s. Duration: %s, Error: %v", volumeID, time.Since(startTime), err)
			return err
		}

		// Determine the target path safely
		target := filepath.Join(destDir, header.Name)
		rel, err := filepath.Rel(destDir, target)
		if err != nil || strings.HasPrefix(rel, "..") {
			return fmt.Errorf("illegal file path in tar archive: %s", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			klog.V(4).Infof("Creating directory %s", target)
			if err := os.MkdirAll(target, 0755); err != nil {
				klog.Warningf("Client: Failed to create dir %s for volume %s. Duration: %s, Error: %v", target, volumeID, time.Since(startTime), err)
				return err
			}
		case tar.TypeReg:
			// Ensure parent directory exists
			klog.V(4).Infof("Creating parent directory %s", filepath.Dir(target))
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				klog.Warningf("Client: Failed to create parent dir for volume %s. Duration: %s, Error: %v", volumeID, time.Since(startTime), err)
				return err
			}

			// Create the file
			klog.V(4).Infof("Creating file %s", target)
			f, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR, os.FileMode(header.Mode))
			if err != nil {
				klog.Warningf("Client: Failed to open/create file %s for volume %s. Duration: %s, Error: %v", target, volumeID, time.Since(startTime), err)
				return err
			}

			// Stream the content from the tar reader to the file
			buf := bufferPool.Get().([]byte)
			_, err = io.CopyBuffer(f, tr, buf)
			bufferPool.Put(buf)
			if err != nil {
				f.Close()
				klog.Warningf("Client: Failed to extract file %s for volume %s. Duration: %s, Error: %v", target, volumeID, time.Since(startTime), err)
				return err
			}
			f.Close()
		}
	}
	duration := time.Since(startTime)
	klog.V(2).Infof("Client: Downloaded volume %s successfully. Size: %s, Duration: %s", volumeID, formatBytes(uint64(reader.bytesRead)), duration)
	return nil
}

func ClientUpload(addr string, volumeDir string, volumeID string) error {
	klog.V(2).Infof("Client: Uploading volume %s starting", volumeID)
	startTime := time.Now()
	conn, _ := grpc.NewClient(
		addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithInitialWindowSize(32*1024*1024),
		grpc.WithInitialConnWindowSize(32*1024*1024),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(64*1024*1024),
			grpc.MaxCallSendMsgSize(64*1024*1024),
		),
	)
	defer conn.Close()
	client := NewFileServiceClient(conn)

	reader, writer := io.Pipe()
	go func() {
		err := createArchive(volumeDir, writer)
		writer.CloseWithError(err)
	}()

	stream, err := client.Upload(context.Background())
	if err != nil {
		err = fmt.Errorf("could not open stream: %v", err)
		klog.Warningf("Client: Failed to upload volume %s. Duration: %s, Error: %v", volumeID, time.Since(startTime), err)
		return err
	}

	// 4. Send the metadata (Volume ID) first
	err = stream.Send(&UploadRequest{
		Data: &UploadRequest_VolumeID{VolumeID: volumeID},
	})
	if err != nil {
		klog.Warningf("Client: Failed to send volume ID %s. Duration: %s, Error: %v", volumeID, time.Since(startTime), err)
		return err
	}

	br := bufio.NewReaderSize(reader, 1024*1024)
	buf := make([]byte, 1024*1024) // 1MB chunks
	for {
		n, err := br.Read(buf)
		if err == io.EOF {
			break
		}
		if err != nil {
			klog.Warningf("Client: Failed to read archive chunk. Volume: %s, Duration: %s, Error: %v", volumeID, time.Since(startTime), err)
			return err
		}

		err = stream.Send(&UploadRequest{
			Data: &UploadRequest_Chunk{Chunk: buf[:n]},
		})
		if err != nil {
			klog.Warningf("Client: Failed to send archive chunk. Volume: %s, Duration: %s, Error: %v", volumeID, time.Since(startTime), err)
			return err
		}
	}

	res, err := stream.CloseAndRecv()
	if err != nil {
		klog.Warningf("Client: Failed to close upload stream. Volume: %s, Duration: %s, Error: %v", volumeID, time.Since(startTime), err)
		return err
	}

	duration := time.Since(startTime)
	klog.V(2).Infof("Client: Uploaded volume %s successfully. Size: %s, Duration: %s", volumeID, formatBytes(res.SizeBytes), duration)
	return nil
}
