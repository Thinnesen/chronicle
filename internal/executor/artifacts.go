package executor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Thinnesen/chronicle/internal/artifact"
)

// The environment variables for artifacts. The credentials come from the Secret of the repository.
const (
	EnvArtifacts = "CHRONICLE_ARTIFACTS"
	EnvAccessKey = "CHRONICLE_ARTIFACT_ACCESS_KEY"
	EnvSecretKey = "CHRONICLE_ARTIFACT_SECRET_KEY"
)

// maxLog is the largest log that the executor archives. Kubernetes keeps less by default (10 MiB).
const maxLog = 32 << 20

// maxArtifact is the largest unpacked size of an input artifact.
const maxArtifact = 8 << 30

// transferTimeout limits all downloads, and then all uploads, of one Pod.
const transferTimeout = 30 * time.Minute

// ArtifactSpec tells the executor which artifacts to download and to save, and if it archives the log.
type ArtifactSpec struct {
	// Repository is nil when the namespace has no artifact repository.
	Repository *artifact.Repository `json:"repository,omitempty"`
	// Prefix is the key prefix of the files of this Pod.
	Prefix  string `json:"prefix,omitempty"`
	Inputs  []File `json:"inputs,omitempty"`
	Outputs []File `json:"outputs,omitempty"`
	// Error is a problem with the repository configuration.
	Error string `json:"error,omitempty"`
}

// File is an input or output artifact. An input comes from Key in the repository, from Raw text or from a URL.
type File struct {
	Name     string  `json:"name"`
	Path     string  `json:"path"`
	Key      string  `json:"key,omitempty"`
	Raw      *string `json:"raw,omitempty"`
	URL      string  `json:"url,omitempty"`
	Mode     *int32  `json:"mode,omitempty"`
	Optional bool    `json:"optional,omitempty"`
}

// fromRepository tells if an input comes from the artifact repository.
func (f File) fromRepository() bool { return f.Raw == nil && f.URL == "" }

// files moves the artifacts and the log of one Pod.
type files struct {
	spec  ArtifactSpec
	store *artifact.Store
	log   *os.File
	logW  *limitWriter
}

// openFiles reads the artifact spec. It returns nil when the Pod has no artifacts and no log archive.
func openFiles() (*files, error) {
	s, ok := os.LookupEnv(EnvArtifacts)
	if !ok {
		return nil, nil
	}
	f := &files{}
	if err := json.Unmarshal([]byte(s), &f.spec); err != nil {
		return nil, fmt.Errorf("bad %s: %w", EnvArtifacts, err)
	}
	r := f.spec.Repository
	if f.spec.Error != "" {
		return nil, errors.New(f.spec.Error)
	}
	if r == nil {
		if f.spec.NeedsRepository() {
			return nil, fmt.Errorf("the namespace has no artifact repository: create the ConfigMap and the Secret %s", artifact.Name)
		}
		return f, nil
	}
	var err error
	if f.store, err = artifact.Open(*r, os.Getenv(EnvAccessKey), os.Getenv(EnvSecretKey)); err != nil {
		return nil, err
	}
	if r.ArchiveLogs {
		if f.log, err = os.Create(filepath.Join(execDir(), "main.log")); err != nil {
			return nil, fmt.Errorf("create the log file: %w", err)
		}
		f.logW = &limitWriter{w: f.log, n: maxLog}
	}
	return f, nil
}

// logWriter returns the writer that archives the log, or io.Discard.
func (f *files) logWriter() io.Writer {
	if f == nil || f.logW == nil {
		return io.Discard
	}
	return f.logW
}

// download puts each input artifact at its path.
func (f *files) download() error {
	if f == nil || len(f.spec.Inputs) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), transferTimeout)
	defer cancel()
	for _, in := range f.spec.Inputs {
		if !in.fromRepository() {
			if err := writeSource(ctx, in); err != nil {
				return fmt.Errorf("input artifact %q: %w", in.Name, err)
			}
			continue
		}
		r, _, err := f.store.Get(ctx, in.Key)
		if err != nil {
			return fmt.Errorf("input artifact %q: %w", in.Name, err)
		}
		err = artifact.Unpack(r, in.Path, maxArtifact)
		_ = r.Close()
		if err != nil {
			return fmt.Errorf("input artifact %q: %w", in.Name, err)
		}
	}
	return nil
}

// save uploads the output artifacts and the log, and adds them to the message.
// After a failed command, a missing output is not an error.
func (f *files) save(msg *Message, succeeded bool, stderr io.Writer) {
	if f == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), transferTimeout)
	defer cancel()
	for _, out := range f.spec.Outputs {
		err := f.saveArtifact(ctx, out)
		switch {
		case err == nil:
			msg.Artifacts = append(msg.Artifacts, out.Name)
		case errors.Is(err, fs.ErrNotExist) && (out.Optional || !succeeded):
		case msg.Error == "":
			msg.Error = fmt.Sprintf("output artifact %q: %v", out.Name, err)
		}
	}
	if f.log != nil {
		if err := f.saveLog(ctx); err != nil {
			_, _ = fmt.Fprintln(stderr, "chronicle-exec: archive the log:", err)
		} else {
			msg.Log = true
		}
	}
}

func (f *files) saveArtifact(ctx context.Context, out File) error {
	if _, err := os.Stat(out.Path); err != nil {
		return err
	}
	pr, pw := io.Pipe()
	go func() { pw.CloseWithError(artifact.Pack(pw, out.Path)) }()
	err := f.store.Put(ctx, artifact.ArtifactKey(f.spec.Prefix, out.Name), pr, -1, "application/gzip")
	_ = pr.CloseWithError(err)
	return err
}

func (f *files) saveLog(ctx context.Context) error {
	if f.logW.dropped {
		_, _ = fmt.Fprintf(f.log, "\n[chronicle: the log is longer than %d MiB; the rest is not archived]\n", maxLog>>20)
	}
	size, err := f.log.Seek(0, io.SeekCurrent)
	if err == nil {
		_, err = f.log.Seek(0, io.SeekStart)
	}
	if err != nil {
		return err
	}
	defer func() { _ = f.log.Close() }()
	return f.store.Put(ctx, artifact.LogKey(f.spec.Prefix), f.log, size, "text/plain; charset=utf-8")
}

// childEnv returns the environment of the main command without the repository credentials.
func childEnv() []string {
	return slices.DeleteFunc(os.Environ(), func(kv string) bool {
		return strings.HasPrefix(kv, EnvAccessKey+"=") || strings.HasPrefix(kv, EnvSecretKey+"=") || strings.HasPrefix(kv, EnvArtifacts+"=")
	})
}

func execDir() string {
	self, err := os.Executable()
	if err != nil {
		return os.TempDir()
	}
	return filepath.Dir(self)
}

// limitWriter writes up to n bytes and drops the rest. It never fails, so that the main command never sees an error.
// The standard output and the standard error write to it at the same time.
type limitWriter struct {
	mu      sync.Mutex
	w       io.Writer
	n       int64
	dropped bool
}

func (l *limitWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if int64(len(p)) > l.n {
		l.dropped = true
		_, _ = l.w.Write(p[:l.n])
		l.n = 0
		return len(p), nil
	}
	_, _ = l.w.Write(p)
	l.n -= int64(len(p))
	return len(p), nil
}
