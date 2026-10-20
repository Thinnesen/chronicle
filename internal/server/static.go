package server

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// contentSecurityPolicy permits only the files of the UI itself: no inline script, no other origin, no frames.
// The UI components inject a few styles, for example the scroll lock of a dialog. Only styles with the nonce of the
// page may do that.
const contentSecurityPolicy = "default-src 'self'; script-src 'self'; style-src 'self' 'nonce-%s'; img-src 'self' data:; " +
	"font-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'; object-src 'none'"

// noncePlaceholder is the nonce in the built index.html. The server puts a new random nonce in its place on each page.
const noncePlaceholder = "__CSP_NONCE__"

// UIHandler serves the built UI. A path without a file gets index.html, so that the router of the UI handles it.
// Files with a content hash in the name are cached for a long time; index.html is not cached. base is the base path of
// the server (see WithBasePath); index.html gets it as its <base href>.
func UIHandler(files fs.FS, base string) http.Handler {
	if files == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "This server was built without the web UI. Use the API or the CLI.", http.StatusNotFound)
		})
	}
	index, err := fs.ReadFile(files, "index.html")
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "The web UI has no index.html.", http.StatusInternalServerError)
		})
	}
	index = bytes.Replace(index, []byte(`<base href="/"`), []byte(`<base href="`+base+`"`), 1)
	static := http.FileServerFS(files)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nonce := newNonce()
		h := w.Header()
		h.Set("Content-Security-Policy", fmt.Sprintf(contentSecurityPolicy, nonce))
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if _, err := fs.Stat(files, name); name == "" || name == "index.html" || err != nil {
			h.Set("Cache-Control", "no-cache")
			h.Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(bytes.ReplaceAll(index, []byte(noncePlaceholder), []byte(nonce)))
			return
		}
		if strings.HasPrefix(name, "assets/") {
			h.Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		static.ServeHTTP(w, r)
	})
}

// newNonce returns 128 random bits in base64.
func newNonce() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b) // crypto/rand.Read never fails.
	return base64.StdEncoding.EncodeToString(b)
}
