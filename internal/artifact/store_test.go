package artifact

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"testing"
	"time"
)

// openTestStore opens the bucket "test" of $CHRONICLE_TEST_S3. See "make s3-up".
func openTestStore(t *testing.T) (*Store, Repository) {
	t.Helper()
	endpoint := os.Getenv("CHRONICLE_TEST_S3")
	if endpoint == "" {
		t.Skip("CHRONICLE_TEST_S3 is not set: run make s3-up")
	}
	repo := Repository{Endpoint: endpoint, Bucket: "test", Insecure: true}
	s, err := Open(repo, "test", "test-secret")
	if err != nil {
		t.Fatal(err)
	}
	// The store creates the bucket a moment after it starts.
	deadline := time.Now().Add(30 * time.Second)
	for {
		err := s.Put(context.Background(), "ready", bytes.NewReader(nil), 0, "text/plain")
		if err == nil {
			return s, repo
		}
		if time.Now().After(deadline) {
			t.Fatalf("the S3 store is not ready: %v", err)
		}
		time.Sleep(time.Second)
	}
}

func TestStore(t *testing.T) {
	s, repo := openTestStore(t)
	ctx := context.Background()
	if err := s.Put(ctx, "a/b.txt", bytes.NewReader([]byte("hello")), -1, "text/plain"); err != nil {
		t.Fatal(err)
	}
	rc, size, err := s.Get(ctx, "a/b.txt")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(rc)
	_ = rc.Close()
	if string(data) != "hello" || size != 5 {
		t.Errorf("got %q (%d bytes)", data, size)
	}
	if _, _, err := s.Get(ctx, "a/missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("a missing object: %v", err)
	}
	wrong, err := Open(repo, "test", "wrong")
	if err != nil {
		t.Fatal(err)
	}
	if err := wrong.Put(ctx, "x", bytes.NewReader(nil), 0, ""); err == nil {
		t.Error("a wrong secret was accepted")
	}
	if _, err := Open(repo, "", ""); err == nil {
		t.Error("empty credentials were accepted")
	}
}

func TestFromConfigMap(t *testing.T) {
	tests := []struct {
		name string
		data map[string]string
		want string
	}{
		{"valid", map[string]string{"endpoint": "s3:443", "bucket": "b", "keyPrefix": "/runs/", "archiveLogs": "true"}, ""},
		{"no port", map[string]string{"endpoint": "https://s3", "bucket": "b"}, "host:port"},
		{"no bucket", map[string]string{"endpoint": "s3:443"}, "bucket is required"},
		{"bad bool", map[string]string{"endpoint": "s3:443", "bucket": "b", "insecure": "maybe"}, "insecure is not true or false"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := FromConfigMap(tt.data)
			switch {
			case tt.want == "" && err != nil:
				t.Fatal(err)
			case tt.want == "" && (r.KeyPrefix != "runs" || !r.ArchiveLogs || r.Prefix("ns", "uid", "pod") != "runs/ns/uid/pod"):
				t.Errorf("repository = %+v", r)
			case tt.want != "" && (err == nil || !bytes.Contains([]byte(err.Error()), []byte(tt.want))):
				t.Errorf("err = %v, want %q", err, tt.want)
			}
		})
	}
}
