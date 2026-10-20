package server

import (
	"net/http"
	"strings"
)

// NormalizeBasePath returns a base path with a slash at the start and at the end, for example "/chronicle/".
func NormalizeBasePath(base string) string {
	base = strings.Trim(base, "/")
	if base == "" {
		return "/"
	}
	return "/" + base + "/"
}

// WithBasePath serves h under a base path, for example "/chronicle/", when a proxy gives chronicle a part of a host. A
// request outside the base path is NotFound, except "/": it goes to the UI.
func WithBasePath(base string, h http.Handler) http.Handler {
	if base == "/" {
		return h
	}
	strip := http.StripPrefix(strings.TrimSuffix(base, "/"), h)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, base):
			strip.ServeHTTP(w, r)
		case r.URL.Path == strings.TrimSuffix(base, "/"):
			http.Redirect(w, r, base, http.StatusMovedPermanently)
		case r.URL.Path == "/":
			http.Redirect(w, r, base, http.StatusFound)
		default:
			http.NotFound(w, r)
		}
	})
}
