package executor

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRunHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer s3cret" || r.Header.Get("X-Run") != "etl" || string(body) != `{"text":"done"}` {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()
	t.Setenv("CHRONICLE_HEADER_0", "Bearer s3cret")
	spec := HTTPSpec{Method: "POST", URL: srv.URL + "/hook?token=abc", Body: `{"text":"done"}`, Headers: []HTTPHeaderSpec{
		{Name: "Authorization", Env: "CHRONICLE_HEADER_0"}, {Name: "X-Run", Value: "etl"},
	}}
	var log bytes.Buffer
	msg := RunHTTP(context.Background(), spec, &log)
	if msg.Error != "" || msg.Result == nil || *msg.Result != "ok" {
		t.Fatalf("message %+v", msg)
	}
	if got := log.String(); !strings.Contains(got, "POST "+srv.URL+"/hook: 200 OK") || strings.Contains(got, "token=abc") || strings.Contains(got, "s3cret") {
		t.Errorf("log %q", got)
	}

	spec.Headers = nil
	msg = RunHTTP(context.Background(), spec, io.Discard)
	if !strings.Contains(msg.Error, "400 Bad Request") || strings.Contains(msg.Error, "token=abc") || msg.Result == nil || *msg.Result != "bad request" {
		t.Errorf("failed request: %+v", msg)
	}
}
