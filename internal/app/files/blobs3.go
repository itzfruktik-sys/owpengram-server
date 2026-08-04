package files

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"

	minio "github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// S3FS stores blob bytes in an S3-compatible object store (self-hosted MinIO
// or AWS S3). It implements BlobBackend using the same content-addressed
// sha256-hex object key scheme as LocalFS, so file_blobs.object_key stays
// comparable regardless of which backend wrote a given row and a deployment
// can run with rows split across both backends (see TELESRV_BLOB_BACKEND).
//
// UploadPartBackend is intentionally NOT implemented here: transient upload
// parts stay on local disk (see cmd/telesrv/main.go) even when the
// permanent blob backend is s3 -- one S3 round trip per ~512KB chunk isn't
// worth it for scratch data that's deleted within minutes of assembly.
type S3FS struct {
	client *minio.Client
	bucket string
}

// NewS3FS creates an S3-compatible blob backend. endpoint is host[:port]
// without a scheme (e.g. "minio.internal:9000" or "s3.amazonaws.com");
// useSSL selects http vs https. pathStyle forces path-style addressing
// (bucket in the URL path rather than as a subdomain), which self-hosted
// MinIO typically requires and AWS S3 does not.
func NewS3FS(ctx context.Context, endpoint, accessKeyID, secretAccessKey, bucket, region string, useSSL, pathStyle bool) (*S3FS, error) {
	if endpoint == "" || bucket == "" {
		return nil, fmt.Errorf("s3 blob backend: endpoint and bucket are required")
	}
	lookup := minio.BucketLookupAuto
	if pathStyle {
		lookup = minio.BucketLookupPath
	}
	client, err := minio.New(endpoint, &minio.Options{
		Creds:        credentials.NewStaticV4(accessKeyID, secretAccessKey, ""),
		Secure:       useSSL,
		Region:       region,
		BucketLookup: lookup,
	})
	if err != nil {
		return nil, fmt.Errorf("create s3 client: %w", err)
	}
	exists, err := client.BucketExists(ctx, bucket)
	if err != nil {
		return nil, fmt.Errorf("check s3 bucket %q: %w", bucket, err)
	}
	if !exists {
		if err := client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{Region: region}); err != nil {
			return nil, fmt.Errorf("create s3 bucket %q: %w", bucket, err)
		}
	}
	return &S3FS{client: client, bucket: bucket}, nil
}

// Name 返回后端标识，与 file_blobs.backend 一致。
func (s *S3FS) Name() string { return "s3" }

func (s *S3FS) Put(ctx context.Context, data []byte) (string, error) {
	key, _, _, err := s.PutReader(ctx, bytes.NewReader(data))
	return key, err
}

// PutReader hashes the stream to a local temp file first (so the sha256 key
// and exact size are known before the S3 PUT, matching LocalFS's
// content-addressed dedup semantics -- an unknown-length streaming PUT would
// need a second copy operation to rename-by-hash after the fact, which S3
// has no equivalent of), then uploads it and checks for an existing object
// with that key first to skip a redundant PUT.
func (s *S3FS) PutReader(ctx context.Context, r io.Reader) (string, int64, []byte, error) {
	tmp, err := os.CreateTemp("", "blob-s3-*.tmp")
	if err != nil {
		return "", 0, nil, fmt.Errorf("create s3 blob staging file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	h := sha256.New()
	size, err := copyWithContext(ctx, io.MultiWriter(tmp, h), r)
	closeErr := tmp.Close()
	if err != nil {
		return "", 0, nil, fmt.Errorf("stage s3 blob: %w", err)
	}
	if closeErr != nil {
		return "", 0, nil, fmt.Errorf("close s3 blob staging file: %w", closeErr)
	}
	sum := h.Sum(nil)
	key := hex.EncodeToString(sum)

	if _, err := s.client.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{}); err == nil {
		return key, size, append([]byte(nil), sum...), nil
	} else if !isS3NotFound(err) {
		return "", 0, nil, fmt.Errorf("stat s3 blob: %w", err)
	}

	f, err := os.Open(tmpPath)
	if err != nil {
		return "", 0, nil, fmt.Errorf("reopen s3 blob staging file: %w", err)
	}
	defer f.Close()
	if _, err := s.client.PutObject(ctx, s.bucket, key, f, size, minio.PutObjectOptions{}); err != nil {
		return "", 0, nil, fmt.Errorf("put s3 blob: %w", err)
	}
	return key, size, append([]byte(nil), sum...), nil
}

func (s *S3FS) Get(ctx context.Context, objectKey string) ([]byte, error) {
	obj, err := s.client.GetObject(ctx, s.bucket, objectKey, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("get s3 blob: %w", err)
	}
	defer obj.Close()
	data, err := io.ReadAll(obj)
	if err != nil {
		return nil, fmt.Errorf("read s3 blob: %w", err)
	}
	return data, nil
}

// GetRange 语义与 LocalFS.GetRange 一致：只读 [offset, offset+limit) 段，limit<=0 读到末尾，
// total 取自对象实际大小。
func (s *S3FS) GetRange(ctx context.Context, objectKey string, offset, limit int64) ([]byte, int64, error) {
	info, err := s.client.StatObject(ctx, s.bucket, objectKey, minio.StatObjectOptions{})
	if err != nil {
		return nil, 0, fmt.Errorf("stat s3 blob: %w", err)
	}
	total := info.Size
	if offset < 0 {
		offset = 0
	}
	if offset >= total {
		return []byte{}, total, nil
	}
	opts := minio.GetObjectOptions{}
	end := total - 1
	if limit > 0 && offset+limit-1 < end {
		end = offset + limit - 1
	}
	if err := opts.SetRange(offset, end); err != nil {
		return nil, 0, fmt.Errorf("set s3 range: %w", err)
	}
	obj, err := s.client.GetObject(ctx, s.bucket, objectKey, opts)
	if err != nil {
		return nil, 0, fmt.Errorf("get s3 blob range: %w", err)
	}
	defer obj.Close()
	data, err := io.ReadAll(obj)
	if err != nil {
		return nil, 0, fmt.Errorf("read s3 blob range: %w", err)
	}
	return data, total, nil
}

// Delete removes the object at objectKey. A missing object is not an error
// (idempotent, safe to retry). Callers must have already confirmed no other
// file_blobs row on this backend still references objectKey.
func (s *S3FS) Delete(ctx context.Context, objectKey string) error {
	if err := s.client.RemoveObject(ctx, s.bucket, objectKey, minio.RemoveObjectOptions{}); err != nil {
		if isS3NotFound(err) {
			return nil
		}
		return fmt.Errorf("delete s3 blob: %w", err)
	}
	return nil
}

func isS3NotFound(err error) bool {
	resp := minio.ToErrorResponse(err)
	return resp.Code == "NoSuchKey" || resp.Code == "NotFound" || resp.StatusCode == 404
}
