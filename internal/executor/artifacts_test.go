package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Thinnesen/chronicle/internal/artifact"
)

// TestArtifacts downloads an input artifact, runs a command that copies it, and saves the output and the log
// in the S3 store of $CHRONICLE_TEST_S3 (see "make s3-up").
func TestArtifacts(t *testing.T) {
	endpoint := os.Getenv("CHRONICLE_TEST_S3")
	if endpoint == "" {
		t.Skip("CHRONICLE_TEST_S3 is not set: run make s3-up")
	}
	repo := artifact.Repository{Endpoint: endpoint, Bucket: "test", Insecure: true, ArchiveLogs: true}
	store, err := artifact.Open(repo, "test", "test-secret")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	src := filepath.Join(t.TempDir(), "in")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "data.txt"), []byte("rows"), 0o644); err != nil {
		t.Fatal(err)
	}
	var packed bytes.Buffer
	if err := artifact.Pack(&packed, src); err != nil {
		t.Fatal(err)
	}
	prefix := "exec-test/" + time.Now().Format("150405.000")
	for i := 0; ; i++ { // The store creates the bucket a moment after it starts.
		if err = store.Put(ctx, prefix+"/input.tgz", bytes.NewReader(packed.Bytes()), int64(packed.Len()), "application/gzip"); err == nil || i == 30 {
			break
		}
		time.Sleep(time.Second)
	}
	if err != nil {
		t.Fatal(err)
	}

	spec, _ := json.Marshal(ArtifactSpec{
		Repository: &repo, Prefix: prefix,
		Inputs:  []File{{Name: "in", Path: "$DIR/in", Key: prefix + "/input.tgz"}},
		Outputs: []File{{Name: "out", Path: "$DIR/out"}, {Name: "maybe", Path: "$DIR/none", Optional: true}},
	})
	env := map[string]string{EnvArtifacts: string(spec), EnvAccessKey: "test", EnvSecretKey: "test-secret"}
	code, out, msg := run(t, `mkdir -p $DIR/out && cp $DIR/in/data.txt $DIR/out/copy.txt && echo "secret is [$CHRONICLE_ARTIFACT_SECRET_KEY]"`, env)
	if code != 0 || msg.Error != "" || !msg.Log || strings.Join(msg.Artifacts, ",") != "out" {
		t.Fatalf("code %d, message %+v", code, msg)
	}
	if out != "secret is []\n" {
		t.Errorf("the command saw the credentials: %q", out)
	}

	rc, _, err := store.Get(ctx, artifact.ArtifactKey(prefix, "out"))
	if err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "check")
	err = artifact.Unpack(rc, dst, 1<<20)
	_ = rc.Close()
	if data, _ := os.ReadFile(filepath.Join(dst, "copy.txt")); err != nil || string(data) != "rows" {
		t.Errorf("output artifact: %v %q", err, data)
	}
	logRC, _, err := store.Get(ctx, artifact.LogKey(prefix))
	if err != nil {
		t.Fatal(err)
	}
	logged, _ := io.ReadAll(logRC)
	_ = logRC.Close()
	if string(logged) != "secret is []\n" {
		t.Errorf("archived log = %q", logged)
	}
}
