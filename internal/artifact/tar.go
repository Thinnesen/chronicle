package artifact

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// root is the name of the archive entry for the artifact path itself: a file, or the top directory.
const root = "."

// Pack writes the file or directory at src as a tar.gz archive. Entry names are relative to src.
// It skips symbolic links and special files, so that an archive never points outside the artifact.
func Pack(w io.Writer, src string) error {
	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() && !d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		hdr := &tar.Header{Name: filepath.ToSlash(rel), Mode: int64(info.Mode().Perm()), ModTime: info.ModTime()}
		if d.IsDir() {
			hdr.Typeflag, hdr.Name = tar.TypeDir, hdr.Name+"/"
			return tw.WriteHeader(hdr)
		}
		hdr.Typeflag, hdr.Size = tar.TypeReg, info.Size()
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		return copyFile(tw, p)
	})
	if err != nil {
		return fmt.Errorf("pack %s: %w", src, err)
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}

func copyFile(w io.Writer, name string) error {
	f, err := os.Open(name)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = io.Copy(w, f)
	return err
}

// ErrTooLarge means that an archive unpacks to more bytes than the limit.
var ErrTooLarge = errors.New("the artifact is larger than the limit")

// Unpack extracts a tar.gz archive from Pack to dst. It accepts only files and directories inside dst. It accepts at
// most limit bytes of file contents, so a small archive cannot fill the disk.
func Unpack(r io.Reader, dst string, limit int64) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("unpack: %w", err)
	}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("unpack: %w", err)
		}
		target, err := entryPath(dst, hdr.Name)
		if err != nil {
			return err
		}
		if hdr.Size > limit {
			return fmt.Errorf("unpack %s: %w", hdr.Name, ErrTooLarge)
		}
		limit -= hdr.Size
		if err := extract(tr, hdr, target); err != nil {
			return fmt.Errorf("unpack %s: %w", hdr.Name, err)
		}
	}
}

// entryPath returns the path of an entry below dst. It refuses absolute names and names with "..".
func entryPath(dst, name string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(strings.TrimSuffix(name, "/")))
	if filepath.IsAbs(clean) || !filepath.IsLocal(clean) && clean != root {
		return "", fmt.Errorf("unpack: the entry %q is outside the artifact", name)
	}
	return filepath.Join(dst, clean), nil
}

func extract(tr *tar.Reader, hdr *tar.Header, target string) error {
	mode := fs.FileMode(hdr.Mode).Perm() | 0o200
	switch hdr.Typeflag {
	case tar.TypeDir:
		return os.MkdirAll(target, mode|0o700)
	case tar.TypeReg:
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		f, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
		if err != nil {
			return err
		}
		if _, err := io.Copy(f, tr); err != nil {
			_ = f.Close()
			return err
		}
		return f.Close()
	}
	return fmt.Errorf("the entry type %q is not supported", hdr.Typeflag)
}
