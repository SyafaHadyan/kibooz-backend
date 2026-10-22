// Package s3 stores uploaded images in any S3 compatible bucket such as Cloudflare R2
package s3

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

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
	// KeyFromURL returns the object key of a public URL this storage handed out, and false for any other URL
	KeyFromURL(url string) (string, bool)
	// PublicURL returns the public URL of an object key
	PublicURL(objectKey string) string
	// PresignUpload returns a URL that accepts one PUT of exactly size bytes of the content type until the ttl passes,
	// so a large file goes straight to the bucket and not through this API
	PresignUpload(ctx context.Context, objectKey string, contentType string, size int64, ttl time.Duration) (string, error)
	// Stat describes a stored object and returns nil when the object does not exist
	Stat(ctx context.Context, objectKey string) (*Object, error)
	// Copy duplicates an object under another key inside the bucket, so the data never passes through this API,
	// and stores the copy with the content type
	Copy(ctx context.Context, sourceKey string, objectKey string, contentType string) error
	// PresignRead returns a URL that reads the object until the ttl passes, for a bucket that is not public
	PresignRead(ctx context.Context, objectKey string, ttl time.Duration) (string, error)
}

// Object describes a stored object
type Object struct {
	Size        int64
	ContentType string
}

// discardTimeout bounds the cleanup of one object
const discardTimeout = 5 * time.Second

// httpTimeout bounds a whole call to the bucket, including the upload of a photo, for a call whose context has no deadline
const httpTimeout = 30 * time.Second

// Discard removes an object that was uploaded for a request that then failed, so no file is left without a record.
// It still runs when the client already gave up and only logs a failure, because the caller is returning another error.
func Discard(ctx context.Context, storage StorageItf, objectKey string) {
	err := TryDiscard(ctx, storage, objectKey)
	if err != nil {
		log.Printf("storage cleanup of %s failed %v", objectKey, err)
	}
}

// TryDiscard is Discard for a caller that wants to know whether the object is gone, so it can try again later
func TryDiscard(ctx context.Context, storage StorageItf, objectKey string) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), discardTimeout)
	defer cancel()

	return storage.Delete(ctx, objectKey)
}

type Storage struct {
	client       *awss3.Client
	presigner    *awss3.PresignClient
	bucket       string
	publicURL    string
	cacheControl string
}

// New returns a working storage when configured, otherwise a disabled one so local development needs no bucket
func New(cfg *env.Env) (StorageItf, error) {
	if !cfg.StorageEnabled() {
		return Disabled{}, nil
	}

	return open(cfg, cfg.S3PublicBucketName, strings.TrimRight(cfg.S3PublicURL, "/"), "public, max-age=31536000, immutable")
}

// NewPrivate returns the storage of the private bucket of the chat media, which has no public address and is read through
// signed addresses. It is disabled when the bucket is not configured.
func NewPrivate(cfg *env.Env) (StorageItf, error) {
	if !cfg.PrivateStorageEnabled() {
		return Disabled{}, nil
	}

	return open(cfg, cfg.S3PrivateBucketName, "", "private, max-age=31536000, immutable")
}

func open(cfg *env.Env, bucket string, publicURL string, cacheControl string) (StorageItf, error) {
	awsCfg, err := awsconfig.LoadDefaultConfig(
		context.Background(),
		// the deadline of a request normally ends a call sooner, this one bounds the calls that do not have one
		awsconfig.WithHTTPClient(&http.Client{Timeout: httpTimeout}),
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
		client:       client,
		presigner:    awss3.NewPresignClient(client),
		bucket:       bucket,
		publicURL:    publicURL,
		cacheControl: cacheControl,
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
		return "", storageFailure(err)
	}

	return s.publicURL + "/" + objectKey, nil
}

func (s *Storage) PublicURL(objectKey string) string {
	// a private bucket has no public address, and a file there is only reached through PresignRead
	if s.publicURL == "" {
		return ""
	}

	return s.publicURL + "/" + objectKey
}

func (s *Storage) PresignUpload(
	ctx context.Context, objectKey string, contentType string, size int64, ttl time.Duration,
) (string, error) {
	// the type and the length are signed, so the bucket refuses any other file than the one that was announced
	req, err := s.presigner.PresignPutObject(ctx, &awss3.PutObjectInput{
		Bucket:        aws.String(s.bucket),
		Key:           aws.String(objectKey),
		ContentType:   aws.String(contentType),
		ContentLength: aws.Int64(size),
	}, awss3.WithPresignExpires(ttl))
	if err != nil {
		// signing is done in this process and never waits for the bucket, so it cannot run out of time
		return "", apperror.ErrStorageFailed.WithErr(err)
	}

	return req.URL, nil
}

func (s *Storage) PresignRead(ctx context.Context, objectKey string, ttl time.Duration) (string, error) {
	req, err := s.presigner.PresignGetObject(ctx, &awss3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(objectKey),
	}, awss3.WithPresignExpires(ttl))
	if err != nil {
		// signing is done in this process and never waits for the bucket, so it cannot run out of time
		return "", apperror.ErrStorageFailed.WithErr(err)
	}

	return req.URL, nil
}

func (s *Storage) Stat(ctx context.Context, objectKey string) (*Object, error) {
	head, err := s.client.HeadObject(ctx, &awss3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(objectKey),
	})
	if err != nil {
		var missing *types.NotFound

		var response *awshttp.ResponseError

		if errors.As(err, &missing) || (errors.As(err, &response) && response.HTTPStatusCode() == http.StatusNotFound) {
			return nil, nil
		}

		return nil, storageFailure(err)
	}

	return &Object{Size: aws.ToInt64(head.ContentLength), ContentType: aws.ToString(head.ContentType)}, nil
}

func (s *Storage) Copy(ctx context.Context, sourceKey string, objectKey string, contentType string) error {
	source := url.URL{Path: s.bucket + "/" + sourceKey}

	_, err := s.client.CopyObject(ctx, &awss3.CopyObjectInput{
		Bucket:     aws.String(s.bucket),
		Key:        aws.String(objectKey),
		CopySource: aws.String(source.EscapedPath()),
		// the copy is described again because the staged file was uploaded without cache headers
		MetadataDirective: types.MetadataDirectiveReplace,
		ContentType:       aws.String(contentType),
		CacheControl:      aws.String(s.cacheControl),
	})
	if err != nil {
		return storageFailure(err)
	}

	return nil
}

func (s *Storage) KeyFromURL(rawURL string) (string, bool) {
	if s.publicURL == "" {
		return "", false
	}

	key, found := strings.CutPrefix(rawURL, s.publicURL+"/")
	if !found || key == "" {
		return "", false
	}

	return key, true
}

func (s *Storage) Delete(ctx context.Context, objectKey string) error {
	_, err := s.client.DeleteObject(ctx, &awss3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(objectKey),
	})
	if err != nil {
		return storageFailure(err)
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

func (Disabled) KeyFromURL(string) (string, bool) {
	return "", false
}

func (Disabled) PublicURL(string) string {
	return ""
}

func (Disabled) PresignUpload(context.Context, string, string, int64, time.Duration) (string, error) {
	return "", apperror.ErrStorageDisabled
}

func (Disabled) PresignRead(context.Context, string, time.Duration) (string, error) {
	return "", apperror.ErrStorageDisabled
}

func (Disabled) Stat(context.Context, string) (*Object, error) {
	return nil, apperror.ErrStorageDisabled
}

func (Disabled) Copy(context.Context, string, string, string) error {
	return apperror.ErrStorageDisabled
}

func (Disabled) Delete(context.Context, string) error {
	return apperror.ErrStorageDisabled
}

// storageFailure is the error of a storage call that failed. A call that ran out of time or was cancelled is a slow bucket and
// answers like any request that ran out of time, which is a 503 that Sentry does not need to hear about, and any other
// failure is a 502.
func storageFailure(err error) error {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return apperror.Internal(err)
	}

	return apperror.ErrStorageFailed.WithErr(err)
}
