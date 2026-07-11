package storage

import (
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"regexp"
	"strings"
	"time"

	go_storage "cloud.google.com/go/storage"
	"github.com/vinhnguyentan99/my-whatsapp/internal/whatsapp"
)

type storageService struct {
	client     *go_storage.Client
	projectID  string
	bucketName string
	cdnBaseUrl string
}

type StorageService interface {
	UploadFile(ctx context.Context, agentID int64, file multipart.File, fileName string) (publicUrl string, mediaType whatsapp.MediaKind, err error)
	DeleteBucketFile(ctx context.Context, fileUrl string) error
}

func NewStorageService(bucketName, bucketProjectID, bucketCdnBaseUrl string) (StorageService, error) {
	ctx := context.Background()

	client, err := go_storage.NewClient(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := client.Bucket(bucketName).Attrs(ctx); err != nil {
		return nil, fmt.Errorf("access bucket %q: %w", bucketName, err)
	}
	if _, err := client.ServiceAccount(ctx, bucketProjectID); err != nil {
		return nil, fmt.Errorf("verify service account: %w", err)
	}

	return storageService{
		client:     client,
		projectID:  bucketProjectID,
		bucketName: bucketName,
		cdnBaseUrl: bucketCdnBaseUrl,
	}, nil
}

// Should add default the agent_id
func (s storageService) UploadFile(ctx context.Context, agentID int64, file multipart.File, fileName string) (publicUrl string, mediaType whatsapp.MediaKind, err error) {
	if file == nil || fileName == "" {
		return "", "", fmt.Errorf("invalid params")
	}

	processedFileName := s.getProcessedFileName(int(agentID), fileName)

	storageWriter := s.client.Bucket(s.bucketName).Object(processedFileName).NewWriter(ctx)
	storageWriter.ACL = []go_storage.ACLRule{{Entity: go_storage.AllUsers, Role: go_storage.RoleReader}}

	if _, err := io.Copy(storageWriter, file); err != nil {
		return "", "", fmt.Errorf("io.Copy: %v", err)
	}
	if err := storageWriter.Close(); err != nil {
		return "", "", fmt.Errorf("Writer.Close: %v", err)
	}

	publicUrl = fmt.Sprintf("%s/%s", s.cdnBaseUrl, processedFileName)
	mediaType = determineMediaType(file)

	return publicUrl, mediaType, nil
}

func (s storageService) DeleteBucketFile(ctx context.Context, bucketFileName string) error {
	obj := s.client.Bucket(s.bucketName).Object(bucketFileName)
	return obj.Delete(ctx)
}

func (s storageService) getProcessedFileName(agentID int, fileName string) string {
	processedFileName := fmt.Sprintf("test%d_%s_%s", agentID, time.Now().UTC().Format("20060102150405"), fileName)
	re := regexp.MustCompile(`[^a-z0-9_\.]+`)
	processedFileName = re.ReplaceAllString(processedFileName, "_")
	return processedFileName
}

func determineMediaType(file multipart.File) whatsapp.MediaKind {
	file.Seek(0, 0) // Reset file read position
	buffer := make([]byte, 512)
	if _, err := file.Read(buffer); err != nil {
		return ""
	}

	contentType := http.DetectContentType(buffer)
	switch {
	case strings.HasPrefix(contentType, "image/"):
		return whatsapp.MediaImage
	case strings.HasPrefix(contentType, "video/"):
		return whatsapp.MediaVideo
	case strings.HasPrefix(contentType, "audio/"):
		return whatsapp.MediaAudio
	default:
		return whatsapp.MediaDocument
	}
}
