package executor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

// EnvHTTP holds the HTTPSpec as JSON.
const EnvHTTP = "CHRONICLE_HTTP"

// httpTimeout limits one request. The template timeout can stop the node earlier.
const httpTimeout = 5 * time.Minute

// maxBody is the largest part of a response body that the executor reads and logs.
const maxBody = 64 << 10

// HTTPSpec is the work of an http node.
type HTTPSpec struct {
	Method  string           `json:"method"`
	URL     string           `json:"url"`
	Body    string           `json:"body,omitempty"`
	Headers []HTTPHeaderSpec `json:"headers,omitempty"`
}

// HTTPHeaderSpec is a header with its value, or with the environment variable that holds a value from a Secret.
type HTTPHeaderSpec struct {
	Name  string `json:"name"`
	Value string `json:"value,omitempty"`
	Env   string `json:"env,omitempty"`
}

// RunHTTP sends the request and returns the message: the response body is the result. A status outside 2xx is
// an error. The log and the error name the URL without its query and user info, because these can hold a token.
func RunHTTP(ctx context.Context, spec HTTPSpec, log io.Writer) Message {
	safe := safeURL(spec.URL)
	method := spec.Method
	if method == "" {
		method = http.MethodGet
	}
	ctx, cancel := context.WithTimeout(ctx, httpTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, spec.URL, strings.NewReader(spec.Body))
	if err != nil {
		return Message{Error: fmt.Sprintf("%s %s: bad request", method, safe)}
	}
	for _, h := range spec.Headers {
		v := h.Value
		if h.Env != "" {
			v = os.Getenv(h.Env)
		}
		req.Header.Set(h.Name, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if uerr, ok := errors.AsType[*url.Error](err); ok {
			err = uerr.Err
		}
		return Message{Error: fmt.Sprintf("%s %s: %v", method, safe, err)}
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return Message{Error: fmt.Sprintf("%s %s: read the response: %v", method, safe, err)}
	}
	status := fmt.Sprintf("%s %s: %s", method, safe, resp.Status)
	_, _ = fmt.Fprintln(log, status)
	_, _ = log.Write(body)
	var msg Message
	if text := strings.TrimSuffix(string(body), "\n"); len(body) <= maxResult {
		msg.Result = &text
	} else {
		msg.ResultTooLarge = true
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		msg.Error = status
	}
	return msg
}

// RunHTTPMain runs the http node of this Pod from the environment and returns the exit code.
func RunHTTPMain(stdout, stderr io.Writer) int {
	var spec HTTPSpec
	if err := json.Unmarshal([]byte(os.Getenv(EnvHTTP)), &spec); err != nil {
		return fail(stderr, fmt.Errorf("bad %s: %w", EnvHTTP, err))
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	msg := fitMessage(RunHTTP(ctx, spec, stdout))
	if err := writeMessage(msg); err != nil {
		_, _ = fmt.Fprintln(stderr, "chronicle-exec:", err)
	}
	if msg.Error != "" {
		_, _ = fmt.Fprintln(stderr, "chronicle-exec:", msg.Error)
		return 1
	}
	return 0
}
