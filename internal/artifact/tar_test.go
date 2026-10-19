package artifact

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPackUnpack(t *testing.T) {
	src := t.TempDir()
	mustWrite(t, filepath.Join(src, "dir", "a.txt"), "alpha")
	mustWrite(t, filepath.Join(src, "file.txt"), "file")
	if err := os.Symlink("/etc/passwd", filepath.Join(src, "dir", "link")); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct{ name, src, check, want string }{
		{"directory", filepath.Join(src, "dir"), "a.txt", "alpha"},
		{"file", filepath.Join(src, "file.txt"), "", "file"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := Pack(&buf, tt.src); err != nil {
				t.Fatal(err)
			}
			dst := filepath.Join(t.TempDir(), "out")
			if err := Unpack(&buf, dst, 1<<20); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(filepath.Join(dst, tt.check))
			if err != nil || string(got) != tt.want {
				t.Errorf("content = %q, %v", got, err)
			}
			if _, err := os.Lstat(filepath.Join(dst, "link")); err == nil {
				t.Error("the symbolic link was packed")
			}
		})
	}
}

func TestUnpackRefusesEntriesOutside(t *testing.T) {
	for _, name := range []string{"../evil", "/etc/evil", "a/../../evil"} {
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		tw := tar.NewWriter(gz)
		_ = tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Size: 1, Mode: 0o644})
		_, _ = tw.Write([]byte("x"))
		_ = tw.Close()
		_ = gz.Close()
		err := Unpack(&buf, t.TempDir(), 1<<20)
		if err == nil || !strings.Contains(err.Error(), "outside the artifact") {
			t.Errorf("entry %q: err = %v", name, err)
		}
	}
}

func mustWrite(t *testing.T, name, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestUnpackLimit(t *testing.T) {
	src := filepath.Join(t.TempDir(), "big")
	mustWrite(t, src, strings.Repeat("x", 1000))
	var buf bytes.Buffer
	if err := Pack(&buf, src); err != nil {
		t.Fatal(err)
	}
	if err := Unpack(&buf, t.TempDir(), 999); !errors.Is(err, ErrTooLarge) {
		t.Errorf("err = %v", err)
	}
}
