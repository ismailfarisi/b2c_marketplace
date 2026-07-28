// Package objectstore issues presigned URLs for direct-to-S3 media transfer.
//
// The API never touches video bytes. A 30–60s vertical reel is 10–30 MB, and
// reading that into a Go handler buys nothing: it doubles the transfer, pins a
// connection and a request goroutine for the whole upload, and turns a slow
// client into backpressure on the order path. Presign, hand the URL to the
// client, and let S3 do what S3 is for.
//
// MinIO speaks the same protocol, which is why local development needs no
// second code path — only a base endpoint and path-style addressing.
package objectstore

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type Config struct {
	Region         string
	Bucket         string
	Endpoint       string // empty for real AWS
	AccessKeyID    string
	SecretKey      string
	ForcePathStyle bool
}

type Store struct {
	client  *s3.Client
	presign *s3.PresignClient
	bucket  string
}

func New(ctx context.Context, cfg Config) (*Store, error) {
	if cfg.Bucket == "" {
		return nil, fmt.Errorf("objectstore: bucket is required")
	}

	loadOpts := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(cfg.Region),
	}
	// Static credentials only when explicitly configured. Otherwise fall through
	// to the default chain so a deployed task uses its IAM role and no long-lived
	// secret exists to leak.
	if cfg.AccessKeyID != "" {
		loadOpts = append(loadOpts, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretKey, ""),
		))
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, loadOpts...)
	if err != nil {
		return nil, fmt.Errorf("objectstore: load aws config: %w", err)
	}

	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if cfg.Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
		}
		// MinIO addresses buckets as a path segment, not a subdomain. Virtual
		// hosting would require wildcard DNS for `bucket.localhost`, which does
		// not resolve.
		o.UsePathStyle = cfg.ForcePathStyle
	})

	return &Store{
		client:  client,
		presign: s3.NewPresignClient(client),
		bucket:  cfg.Bucket,
	}, nil
}

func (s *Store) Bucket() string { return s.bucket }

type PresignedUpload struct {
	Key       string
	URL       string
	ExpiresAt time.Time
	// Headers the client MUST send with the PUT. A presigned URL signs the
	// headers it was created with; sending a different Content-Type produces a
	// SignatureDoesNotMatch that reads like an auth bug and is not one.
	Headers map[string]string
}

// PresignUpload returns a URL the client PUTs the raw video to.
//
// The key is generated here and never accepted from the client. A
// client-supplied key is a path traversal, an overwrite of another vendor's
// object, and a way to make the bucket layout unpredictable — all for the
// convenience of not generating eight bytes of randomness.
func (s *Store) PresignUpload(ctx context.Context, ownerType string, ownerID, contentType string, ttl time.Duration) (PresignedUpload, error) {
	key, err := uploadKey(ownerType, ownerID, contentType)
	if err != nil {
		return PresignedUpload{}, err
	}

	req, err := s.presign.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(key),
		ContentType: aws.String(contentType),
	}, s3.WithPresignExpires(ttl))
	if err != nil {
		return PresignedUpload{}, fmt.Errorf("objectstore: presign put: %w", err)
	}

	return PresignedUpload{
		Key:       key,
		URL:       req.URL,
		ExpiresAt: time.Now().Add(ttl),
		Headers:   map[string]string{"Content-Type": contentType},
	}, nil
}

// PresignDownload is for the moderation worker reading the source object and
// for admin playback of unpublished content. Published delivery goes through
// the CDN, never through a presigned URL — a signed URL per viewer defeats edge
// caching entirely.
func (s *Store) PresignDownload(ctx context.Context, key string, ttl time.Duration) (string, error) {
	req, err := s.presign.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(ttl))
	if err != nil {
		return "", fmt.Errorf("objectstore: presign get: %w", err)
	}
	return req.URL, nil
}

// uploadKey lays out `uploads/{ownerType}/{ownerID}/{random}.{ext}`.
//
// Owner-prefixed so a bucket policy can scope access per tenant later without a
// data migration, and randomly suffixed so two uploads from the same vendor in
// the same second cannot collide.
func uploadKey(ownerType, ownerID, contentType string) (string, error) {
	var buf [12]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("objectstore: generate key: %w", err)
	}

	ext := extensionFor(contentType)
	if ext == "" {
		return "", fmt.Errorf("objectstore: unsupported content type %q", contentType)
	}

	return path.Join("uploads", ownerType, ownerID, hex.EncodeToString(buf[:])+ext), nil
}

// extensionFor doubles as the allow-list. An unrecognised type never gets a
// presigned URL, so an unsupported upload is refused before it costs any
// transfer — the probe gate in §2.3 is the second line, not the first.
func extensionFor(contentType string) string {
	switch strings.ToLower(strings.TrimSpace(contentType)) {
	case "video/mp4":
		return ".mp4"
	case "video/quicktime":
		return ".mov"
	case "video/webm":
		return ".webm"
	default:
		return ""
	}
}
