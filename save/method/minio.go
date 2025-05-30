package method

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"

	"github.com/beck-8/subs-check/config"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// ValiMinioConfig checks if the MinIO configuration is complete.
func ValiMinioConfig() error {
	if config.GlobalConfig.MinioEndpoint == "" {
		return fmt.Errorf("MinioEndpoint is not configured")
	}
	if config.GlobalConfig.MinioAccessID == "" {
		return fmt.Errorf("MinioAccessID is not configured")
	}
	if config.GlobalConfig.MinioSecretKey == "" {
		return fmt.Errorf("MinioSecretKey is not configured")
	}
	if config.GlobalConfig.MinioBucket == "" {
		return fmt.Errorf("MinioBucket is not configured")
	}
	return nil
}

// UploadToMinio uploads data to a MinIO bucket.
// The 'filename' parameter will be used as the object name in the bucket.
func UploadToMinio(data []byte, filename string) error {
	ctx := context.Background()
	endpoint := config.GlobalConfig.MinioEndpoint
	accessKeyID := config.GlobalConfig.MinioAccessID
	secretAccessKey := config.GlobalConfig.MinioSecretKey
	useSSL := config.GlobalConfig.MinioUseSSL // e.g., true for HTTPS, false for HTTP
	bucketName := config.GlobalConfig.MinioBucket

	// Initialize minio client object.
	minioClient, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKeyID, secretAccessKey, ""),
		Secure: useSSL,
	})
	if err != nil {
		return fmt.Errorf("failed to initialize MinIO client: %w", err)
	}

	// Check if the bucket exists.
	exists, err := minioClient.BucketExists(ctx, bucketName)
	if err != nil {
		return fmt.Errorf("failed to check if bucket '%s' exists: %w", bucketName, err)
	}
	if !exists {
		return fmt.Errorf("bucket '%s' does not exist", bucketName)
	}

	// Upload the data.
	reader := bytes.NewReader(data)
	objectName := filename
	contentType := "application/octet-stream"

	info, err := minioClient.PutObject(ctx, bucketName, objectName, reader, int64(len(data)), minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		return fmt.Errorf("failed to upload '%s' to bucket '%s': %w", objectName, bucketName, err)
	}

	slog.Info("Successfully uploaded '%s' of size %d to bucket '%s'. ETag: %s", objectName, info.Size, bucketName, info.ETag)
	return nil
}
