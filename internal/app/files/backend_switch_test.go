package files

import (
	"context"
	"os"
	"testing"

	"telesrv/internal/domain"
)

// fakeNamedBackend is a minimal BlobBackend double with a settable Name(),
// used to prove Service.backendFor's routing logic without needing two real
// backend implementations (LocalFS always reports "localfs").
type fakeNamedBackend struct {
	BlobBackend
	name string
}

func (f fakeNamedBackend) Name() string { return f.name }

// TestBackendForRoutesToTheBackendThatActuallyWroteTheBlob proves the core
// safety property behind switching TELESRV_BLOB_BACKEND: a row whose
// recorded backend differs from the currently active one must still
// resolve to the backend that actually holds its bytes (via
// WithAdditionalBlobBackend), not silently fall through to the active one
// (which would 404 -- the row/bytes still exist, but the code would be
// looking in the wrong place).
func TestBackendForRoutesToTheBackendThatActuallyWroteTheBlob(t *testing.T) {
	active := fakeNamedBackend{name: "s3"}
	old := fakeNamedBackend{name: "localfs"}

	s := NewService(nil, active, 2, WithAdditionalBlobBackend(old))

	got, err := s.backendFor(domain.MediaBackendS3)
	if err != nil || got.Name() != "s3" {
		t.Fatalf("resolve active backend: got %v err=%v, want s3", got, err)
	}
	got, err = s.backendFor(domain.MediaBackendLocalFS)
	if err != nil || got.Name() != "localfs" {
		t.Fatalf("resolve registered old backend: got %v err=%v, want localfs", got, err)
	}
	// A row predating the owner/backend column (empty string) must not be
	// treated as unreachable -- it falls back to the active backend, which
	// is where every pre-migration row's bytes actually are.
	got, err = s.backendFor("")
	if err != nil || got.Name() != "s3" {
		t.Fatalf("resolve empty/legacy backend: got %v err=%v, want active s3", got, err)
	}
	// A backend that was never configured (e.g. its config was wiped after
	// switching away from it) must fail loudly, not silently read from the
	// wrong place.
	if _, err := s.backendFor("gcs"); err == nil {
		t.Fatal("expected an error resolving an unconfigured backend, got nil")
	}
}

// TestServiceGetFileReadsFromRegisteredOldBackendAfterSwitch is the
// end-to-end version: a Service whose active backend is A must still serve
// a file whose file_blobs row says it lives on B, as long as B was
// registered via WithAdditionalBlobBackend -- exactly the "I switched
// TELESRV_BLOB_BACKEND, did my old files disappear?" scenario.
func TestServiceGetFileReadsFromRegisteredOldBackendAfterSwitch(t *testing.T) {
	ctx := context.Background()
	oldBackend, err := NewLocalFS(t.TempDir())
	if err != nil {
		t.Fatalf("new old backend: %v", err)
	}
	newBackend, err := NewLocalFS(t.TempDir())
	if err != nil {
		t.Fatalf("new active backend: %v", err)
	}
	// Give the "old" backend a distinct name so backendFor can tell them
	// apart, the way a real second BlobBackend implementation would.
	oldNamed := fakeNamedBackend{BlobBackend: oldBackend, name: "old-backend"}

	data := []byte("file written before the backend switch")
	objectKey, err := oldBackend.Put(ctx, data)
	if err != nil {
		t.Fatalf("write to old backend: %v", err)
	}

	media := newFakeMediaStore()
	if err := media.PutFileBlob(ctx, domain.FileBlob{
		LocationKey: "doc:1",
		Backend:     "old-backend",
		ObjectKey:   objectKey,
		Size:        int64(len(data)),
	}); err != nil {
		t.Fatalf("put file blob metadata: %v", err)
	}

	// Active backend is the NEW one; the old one is only reachable via the
	// registry -- mirrors production after a TELESRV_BLOB_BACKEND switch.
	s := NewService(media, newBackend, 2, WithAdditionalBlobBackend(oldNamed))

	chunk, found, err := s.GetFile(ctx, domain.FileDownloadRequest{LocationKey: "doc:1", Limit: 1 << 20})
	if err != nil {
		t.Fatalf("get file after backend switch: %v", err)
	}
	if !found {
		t.Fatal("file written before the switch was not found -- it would appear to have vanished")
	}
	if string(chunk.Bytes) != string(data) {
		t.Fatalf("chunk = %q, want %q", chunk.Bytes, data)
	}
}

// TestBackendSwitchRoundTripsThroughRealMinIO exercises the actual
// scenario the user asked about: files written while s3 (MinIO) was active
// stay readable after switching to localfs, and files written while
// localfs was active stay readable after switching to s3 -- both directions,
// against a real MinIO instance (deploy/docker-compose.yml's "minio"
// service). Skips if MinIO isn't reachable at the default dev endpoint.
func TestBackendSwitchRoundTripsThroughRealMinIO(t *testing.T) {
	endpoint := os.Getenv("TELESRV_TEST_S3_ENDPOINT")
	if endpoint == "" {
		endpoint = "localhost:9000"
	}
	ctx := context.Background()
	s3, err := NewS3FS(ctx, endpoint, "owpengram", "owpengram123", "telesrv-test-backend-switch", "us-east-1", false, true)
	if err != nil {
		t.Skipf("minio not reachable at %s (start deploy/docker-compose.yml's minio service to run this test): %v", endpoint, err)
	}
	local, err := NewLocalFS(t.TempDir())
	if err != nil {
		t.Fatalf("new local backend: %v", err)
	}

	t.Run("s3_active_localfs_switch", func(t *testing.T) {
		s3Data := []byte("written while s3 was the active backend")
		s3Key, err := s3.Put(ctx, s3Data)
		if err != nil {
			t.Fatalf("write to s3: %v", err)
		}
		t.Cleanup(func() { _ = s3.Delete(ctx, s3Key) })

		media := newFakeMediaStore()
		if err := media.PutFileBlob(ctx, domain.FileBlob{
			LocationKey: "doc:1", Backend: domain.MediaBackendS3, ObjectKey: s3Key, Size: int64(len(s3Data)),
		}); err != nil {
			t.Fatalf("put file blob: %v", err)
		}

		// Switched: localfs is now active, s3 kept reachable as the old backend.
		s := NewService(media, local, 2, WithAdditionalBlobBackend(s3))
		chunk, found, err := s.GetFile(ctx, domain.FileDownloadRequest{LocationKey: "doc:1", Limit: 1 << 20})
		if err != nil || !found || string(chunk.Bytes) != string(s3Data) {
			t.Fatalf("read s3-written file after switching to localfs: found=%v err=%v bytes=%q, want %q", found, err, chunk.Bytes, s3Data)
		}
	})

	t.Run("localfs_active_s3_switch", func(t *testing.T) {
		localData := []byte("written while localfs was the active backend")
		localKey, err := local.Put(ctx, localData)
		if err != nil {
			t.Fatalf("write to localfs: %v", err)
		}

		media := newFakeMediaStore()
		if err := media.PutFileBlob(ctx, domain.FileBlob{
			LocationKey: "doc:2", Backend: domain.MediaBackendLocalFS, ObjectKey: localKey, Size: int64(len(localData)),
		}); err != nil {
			t.Fatalf("put file blob: %v", err)
		}

		// Switched: s3 is now active, localfs kept reachable as the old backend.
		s := NewService(media, s3, 2, WithAdditionalBlobBackend(local))
		chunk, found, err := s.GetFile(ctx, domain.FileDownloadRequest{LocationKey: "doc:2", Limit: 1 << 20})
		if err != nil || !found || string(chunk.Bytes) != string(localData) {
			t.Fatalf("read localfs-written file after switching to s3: found=%v err=%v bytes=%q, want %q", found, err, chunk.Bytes, localData)
		}
	})
}
