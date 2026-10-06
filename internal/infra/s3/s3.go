// Package s3 stores uploaded images in any S3 compatible bucket such as Cloudflare R2
package s3

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/env"
)

type StorageItf interface {
	// Enabled reports whether uploads can succeed
	Enabled() bool
	// Upload stores the object and returns its public URL
	Upload(ctx context.Context, objectKey string, contentType string, data []byte) (string, error)
	// Delete removes the object, and a key that does not exist is not an error
	Delete(ctx context.Context, objectKey string) error
}

// discardTimeout bounds the cleanup of one object
const discardTimeout = 5 * time.Second

// Discard removes an object that was uploaded for a request that then failed, so no file is left without a record.
// It still runs when the client already gave up and only logs a failure, because the caller is returning another error.
func Discard(ctx context.Context, storage StorageItf, objectKey string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), discardTimeout)
	defer cancel()

	err := storage.Delete(ctx, objectKey)
	if err != nil {
		log.Printf("storage cleanup of %s failed %v", objectKey, err)
	}
}

type Storage struct {
	client    *awss3.Client
	bucket    string
	publicURL string
}

// New returns a working storage when configured, otherwise a disabled one so local development needs no bucket
func New(cfg *env.Env) (StorageItf, error) {
	if !cfg.StorageEnabled() {
		return Disabled{}, nil
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(
		context.Background(),
		awsconfig.WithRegion(cfg.S3Region),
		awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.S3AccessKeyID, cfg.S3AccessKeySecret, ""),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("load s3 config: %w", err)
	}

	endpoint := cfg.S3Endpoint
	if endpoint == "" {
		endpoint = fmt.Sprintf("https://%s.r2.cloudflarestorage.com", cfg.S3AccountID)
	}

	client := awss3.NewFromConfig(awsCfg, func(o *awss3.Options) {
		o.BaseEndpoint = aws.String(endpoint)
		o.UsePathStyle = cfg.S3Endpoint != ""
		// S3 compatible stores such as R2 do not accept the SDK default checksum headers
		o.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
		o.ResponseChecksumValidation = aws.ResponseChecksumValidationWhenRequired
	})

	return &Storage{
		client:    client,
		bucket:    cfg.S3BucketName,
		publicURL: strings.TrimRight(cfg.S3PublicURL, "/"),
	}, nil
}

func (s *Storage) Enabled() bool {
	return true
}

func (s *Storage) Upload(ctx context.Context, objectKey string, contentType string, data []byte) (string, error) {
	_, err := s.client.PutObject(ctx, &awss3.PutObjectInput{
		Bucket:       aws.String(s.bucket),
		Key:          aws.String(objectKey),
		Body:         bytes.NewReader(data),
		ContentType:  aws.String(contentType),
		CacheControl: aws.String("public, max-age=31536000, immutable"),
	})
	if err != nil {
		return "", apperror.ErrStorageFailed.WithErr(err)
	}

	return s.publicURL + "/" + objectKey, nil
}

func (s *Storage) Delete(ctx context.Context, objectKey string) error {
	_, err := s.client.DeleteObject(ctx, &awss3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(objectKey),
	})
	if err != nil {
		return apperror.ErrStorageFailed.WithErr(err)
	}

	return nil
}

// Disabled is used when no bucket is configured
type Disabled struct{}

func (Disabled) Enabled() bool {
	return false
}

func (Disabled) Upload(context.Context, string, string, []byte) (string, error) {
	return "", apperror.ErrStorageDisabled
}

func (Disabled) Delete(context.Context, string) error {
	return apperror.ErrStorageDisabled
}
