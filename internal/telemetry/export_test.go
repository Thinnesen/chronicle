package telemetry

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace/noop"
	coltrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/grpc"
)

// collector is an OTLP trace receiver that keeps the spans it gets.
type collector struct {
	coltrace.UnimplementedTraceServiceServer
	mu    sync.Mutex
	spans map[string][]byte // span name to trace ID
}

func (c *collector) Export(_ context.Context, req *coltrace.ExportTraceServiceRequest) (*coltrace.ExportTraceServiceResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, rs := range req.ResourceSpans {
		for _, ss := range rs.ScopeSpans {
			for _, s := range ss.Spans {
				c.spans[s.Name] = s.TraceId
			}
		}
	}
	return &coltrace.ExportTraceServiceResponse{}, nil
}

// TestExport sends the spans of a run through the real OTLP exporter to a receiver.
func TestExport(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	c := &collector{spans: map[string][]byte{}}
	srv := grpc.NewServer()
	coltrace.RegisterTraceServiceServer(srv, c)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://"+lis.Addr().String())
	t.Setenv("OTEL_EXPORTER_OTLP_INSECURE", "true")
	stop, err := Setup(context.Background(), "test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { otel.SetTracerProvider(noop.NewTracerProvider()) })
	start := time.Now()
	EmitRun(context.Background(), otel.Tracer("test"), uid, NodeSpan{ID: "0", Name: "etl-x7", Start: start, End: start.Add(time.Second)},
		[]NodeSpan{{ID: "0/load", Name: "etl-x7.load", Start: start, End: start.Add(time.Second)}})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := stop(ctx); err != nil {
		t.Fatal(err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	want := TraceID(uid)
	for _, name := range []string{"etl-x7", "etl-x7.load"} {
		if got := c.spans[name]; string(got) != string(want[:]) {
			t.Errorf("span %s: trace ID %x, want %s", name, got, want)
		}
	}
}
