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
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

type Config struct {
	Region   string
	Bucket   string
	Endpoint string // empty for real AWS
	// PublicEndpoint is the host *clients* reach the store on, when that is not
	// the host this process reaches it on. Empty means they are the same.
	//
	// They are routinely not the same, and assuming they are is a bug that only
	// shows up once a real device is involved. A deployed API talks to S3 over a
	// VPC endpoint or an internal service name; the phone has to be handed a
	// publicly resolvable host. In development the split is starker still: the
	// API reaches MinIO at localhost:9000, and an Android emulator reaching
	// "localhost" reaches *itself* — its route to the host machine is 10.0.2.2,
	// which in turn is not routable from the host.
	//
	// Signing has to happen against the endpoint the client will call, because
	// the host is part of what SigV4 signs. So there are two clients: one for
	// this process's own requests, one that exists only to mint URLs.
	PublicEndpoint string
	AccessKeyID    string
	SecretKey      string
	ForcePathStyle bool
}

type Store struct {
	client *s3.Client
	// presign may be backed by a different endpoint than client — see
	// Config.PublicEndpoint.
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

	newClient := func(endpoint string) *s3.Client {
		return s3.NewFromConfig(awsCfg, func(o *s3.Options) {
			if endpoint != "" {
				o.BaseEndpoint = aws.String(endpoint)
			}
			// MinIO addresses buckets as a path segment, not a subdomain. Virtual
			// hosting would require wildcard DNS for `bucket.localhost`, which does
			// not resolve.
			o.UsePathStyle = cfg.ForcePathStyle
		})
	}

	client := newClient(cfg.Endpoint)

	// The signing client, which never sends a request — it only produces URLs,
	// and those have to name the host the *client* will call.
	signer := client
	if cfg.PublicEndpoint != "" && cfg.PublicEndpoint != cfg.Endpoint {
		signer = newClient(cfg.PublicEndpoint)
	}

	return &Store{
		client:  client,
		presign: s3.NewPresignClient(signer),
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

// Put writes an object directly.
//
// The exception to this package's rule, and worth stating why it is allowed.
// The rule — *the API never touches video bytes* — is about the **request
// path**: a presigned URL keeps a slow uploader from pinning a handler
// goroutine, and nothing about that reasoning applies to a worker that has just
// produced a two-hundred-kilobyte HLS segment on its own disk.
//
// So this is for outputs, not inputs. Every caller is in the worker; nothing on
// the request path may use it, and a handler that does has misread the comment
// above.
func (s *Store) Put(ctx context.Context, key, contentType string, body io.Reader) error {
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(key),
		Body:        body,
		ContentType: aws.String(contentType),
	})
	if err != nil {
		return fmt.Errorf("objectstore: put %s: %w", key, err)
	}
	return nil
}

// Size reports how many bytes are at a key, and ok=false when nothing is.
//
// A HEAD, not a GET — this reads the object's metadata and never its body, so it
// does not breach the rule above about the request path. Its one caller is the
// publish handler confirming that the video the client says it uploaded is
// actually in the bucket.
func (s *Store) Size(ctx context.Context, key string) (int64, bool, error) {
	out, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		// A missing object is the normal answer to this question — the client
		// may still be uploading — so it is reported as ok=false rather than as
		// an error the caller has to string-match.
		var missing *types.NotFound
		var noKey *types.NoSuchKey
		if errors.As(err, &missing) || errors.As(err, &noKey) {
			return 0, false, nil
		}
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) && (apiErr.ErrorCode() == "NotFound" || apiErr.ErrorCode() == "NoSuchKey") {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("objectstore: head %s: %w", key, err)
	}
	if out.ContentLength == nil {
		return 0, true, nil
	}
	return *out.ContentLength, true, nil
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
