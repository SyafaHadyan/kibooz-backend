package bucketsizes

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
)

// httpTimeout bounds one call to the store, and one scan has a time of its own on top of that
const httpTimeout = 60 * time.Second

// S3Lister lists the objects of the buckets of any S3 compatible store, page by page
type S3Lister struct {
	client *awss3.Client
}

// S3Settings is how to reach the store and which key to use
type S3Settings struct {
	Endpoint        string
	Region          string
	AccessKeyID     string
	AccessKeySecret string
	// PathStyle puts the bucket in the path and not in the host name, which every store that this is meant for accepts
	PathStyle bool
}

// NewS3Lister builds the client of the store. It does not call it.
func NewS3Lister(ctx context.Context, settings S3Settings) (*S3Lister, error) {
	if settings.Endpoint == "" || settings.AccessKeyID == "" || settings.AccessKeySecret == "" {
		return nil, errors.New("the endpoint and the access key of the store are needed")
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(
		ctx,
		awsconfig.WithHTTPClient(&http.Client{Timeout: httpTimeout}),
		awsconfig.WithRegion(settings.Region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(settings.AccessKeyID, settings.AccessKeySecret, "")),
	)
	if err != nil {
		return nil, fmt.Errorf("load the s3 config %w", err)
	}

	client := awss3.NewFromConfig(awsCfg, func(o *awss3.Options) {
		o.BaseEndpoint = aws.String(settings.Endpoint)
		o.UsePathStyle = settings.PathStyle
		// S3 compatible stores such as R2 do not accept the SDK default checksum headers
		o.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
		o.ResponseChecksumValidation = aws.ResponseChecksumValidationWhenRequired
	})

	return &S3Lister{client: client}, nil
}

// Totals adds up the size and the number of the objects of the bucket. It reads every page, 1000 objects at a time, and stops at the
// first error, because a half counted bucket would show a size that is too small.
func (l *S3Lister) Totals(ctx context.Context, bucket string) (Totals, error) {
	var totals Totals

	// a store that answers the same continuation token twice would otherwise be listed until the timeout
	pages := awss3.NewListObjectsV2Paginator(l.client, &awss3.ListObjectsV2Input{Bucket: aws.String(bucket)}, func(o *awss3.ListObjectsV2PaginatorOptions) {
		o.StopOnDuplicateToken = true
	})

	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return Totals{}, fmt.Errorf("list the bucket %s %w", bucket, err)
		}

		for _, object := range page.Contents {
			totals.Bytes += aws.ToInt64(object.Size)
			totals.Objects++
		}
	}

	return totals, nil
}
