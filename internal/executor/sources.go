package executor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// InstallMain copies the executor to dst for the main container, and downloads the input artifacts when the
// environment has them. It returns the exit code. A failed download leaves its error in the termination message.
func InstallMain(dst string, stderr io.Writer) int {
	if err := Install(dst); err != nil {
		return fail(stderr, err)
	}
	f, err := openFiles()
	if err == nil {
		err = f.download()
	}
	if err != nil {
		return fail(stderr, err)
	}
	return 0
}

// NeedsRepository tells if the Pod reads or writes the artifact repository. Raw and http inputs do not.
func (s ArtifactSpec) NeedsRepository() bool {
	for _, in := range s.Inputs {
		if in.fromRepository() {
			return true
		}
	}
	return len(s.Outputs) > 0
}

// writeSource writes a raw or http input artifact to its path, as one file with its mode.
func writeSource(ctx context.Context, in File) error {
	if err := os.MkdirAll(filepath.Dir(in.Path), 0o755); err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if in.Mode != nil {
		mode = os.FileMode(*in.Mode) & os.ModePerm
	}
	file, err := os.OpenFile(in.Path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if in.Raw != nil {
		_, err = io.WriteString(file, *in.Raw)
	} else {
		err = download(ctx, in.URL, file)
	}
	if cerr := file.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		// The umask of the container can remove bits from the mode of a new file.
		err = os.Chmod(in.Path, mode)
	}
	return err
}

// download writes the body of a GET request. Its errors name the URL without the query and the user info,
// because a signed URL has its token there.
func download(ctx context.Context, rawURL string, w io.Writer) error {
	safe := safeURL(rawURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return fmt.Errorf("download %s: bad URL", safe)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if uerr, ok := errors.AsType[*url.Error](err); ok {
			err = uerr.Err
		}
		return fmt.Errorf("download %s: %w", safe, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: %s", safe, resp.Status)
	}
	n, err := io.Copy(w, io.LimitReader(resp.Body, maxArtifact+1))
	if err != nil {
		return fmt.Errorf("download %s: %w", safe, err)
	}
	if n > maxArtifact {
		return fmt.Errorf("download %s: the file is larger than %d GiB", safe, maxArtifact>>30)
	}
	return nil
}

func safeURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "(a bad URL)"
	}
	return strings.TrimSuffix(u.Scheme+"://"+u.Host+u.Path, "/")
}
