package s3

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
)

// AWSConfig is the production wiring for ObjectClient backed by
// aws-sdk-go-v2/service/s3. The same struct covers AWS S3 and any
// S3-compatible service (MinIO, Ceph, Backblaze, …) — set Endpoint +
// UsePathStyle for non-AWS deployments.
type AWSConfig struct {
	// Region is the AWS region. Required for AWS S3; for MinIO the
	// region is largely cosmetic but the SDK still requires a value
	// — "us-east-1" is the conventional default.
	Region string
	// Endpoint, if non-empty, overrides the default AWS S3 endpoint.
	// MinIO sites set this to e.g. "http://minio:9000".
	Endpoint string
	// UsePathStyle forces path-style addressing
	// ("https://endpoint/bucket/key" instead of
	// "https://bucket.endpoint/key"). REQUIRED for MinIO and many
	// other S3-compatible services that don't terminate TLS for
	// per-bucket subdomains.
	UsePathStyle bool
	// AccessKey + SecretKey are static IAM-style credentials. Leave
	// both empty to fall back on the default credential chain
	// (environment, shared config, IAM role, …).
	AccessKey string
	SecretKey string
	// Bucket is captured here as a default for the wrapped client; it
	// MUST match the Bucket in s3.Config so the connector talks to
	// the same bucket the AWS SDK is wired against.
	Bucket string
}

// NewAWSClient builds an aws-sdk-go-v2 S3 client and wraps it as an
// ObjectClient. The returned client respects ctx for both the initial
// LoadDefaultConfig call (credential chain probing can do network
// I/O) and every subsequent ListObjects / GetObject call.
//
// The function is INTENTIONALLY thin — the package's value lives in
// the format-decoding + cursor-pagination layer; the SDK adapter is
// here so production callers don't have to reproduce the
// custom-endpoint + path-style boilerplate that MinIO requires.
func NewAWSClient(ctx context.Context, cfg AWSConfig) (ObjectClient, error) {
	if cfg.Bucket == "" {
		return nil, errors.New("s3: AWSConfig.Bucket must not be empty")
	}
	loadOpts := []func(*awsconfig.LoadOptions) error{}
	if cfg.Region != "" {
		loadOpts = append(loadOpts, awsconfig.WithRegion(cfg.Region))
	}
	if cfg.AccessKey != "" || cfg.SecretKey != "" {
		loadOpts = append(loadOpts, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, ""),
		))
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, loadOpts...)
	if err != nil {
		return nil, fmt.Errorf("s3: load aws config: %w", err)
	}
	clientOpts := []func(*awss3.Options){}
	if cfg.Endpoint != "" {
		endpoint := cfg.Endpoint
		clientOpts = append(clientOpts, func(o *awss3.Options) {
			o.BaseEndpoint = aws.String(endpoint)
		})
	}
	if cfg.UsePathStyle {
		clientOpts = append(clientOpts, func(o *awss3.Options) {
			o.UsePathStyle = true
		})
	}
	s3client := awss3.NewFromConfig(awsCfg, clientOpts...)
	return &awsObjectClient{client: s3client, bucket: cfg.Bucket}, nil
}

// awsObjectClient adapts the aws-sdk-go-v2/service/s3 client to the
// connector's ObjectClient interface. The bucket is captured at
// construction so callers don't have to repeat it on every call.
type awsObjectClient struct {
	client *awss3.Client
	bucket string
}

func (a *awsObjectClient) ListObjects(ctx context.Context, prefix string, continuationToken string) ([]ObjectInfo, string, error) {
	in := &awss3.ListObjectsV2Input{Bucket: aws.String(a.bucket)}
	if prefix != "" {
		in.Prefix = aws.String(prefix)
	}
	if continuationToken != "" {
		in.ContinuationToken = aws.String(continuationToken)
	}
	out, err := a.client.ListObjectsV2(ctx, in)
	if err != nil {
		return nil, "", err
	}
	objects := make([]ObjectInfo, 0, len(out.Contents))
	for _, obj := range out.Contents {
		key := ""
		if obj.Key != nil {
			key = *obj.Key
		}
		var size int64
		if obj.Size != nil {
			size = *obj.Size
		}
		objects = append(objects, ObjectInfo{Key: key, Size: size})
	}
	next := ""
	if out.NextContinuationToken != nil && (out.IsTruncated != nil && *out.IsTruncated) {
		next = *out.NextContinuationToken
	}
	return objects, next, nil
}

func (a *awsObjectClient) GetObject(ctx context.Context, key string) ([]byte, error) {
	out, err := a.client.GetObject(ctx, &awss3.GetObjectInput{
		Bucket: aws.String(a.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, err
	}
	defer out.Body.Close()
	return io.ReadAll(out.Body)
}
