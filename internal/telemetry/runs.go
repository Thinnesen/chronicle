package telemetry

import (
	"cmp"
	"context"
	crand "crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// Span is one finished span with its times from the status of a run.
type Span struct {
	Name       string
	Start, End time.Time
	Attributes map[string]string
	Error      string
}

// NodeSpan is a span with its key. Parent is the key of the parent span, or empty for the run.
type NodeSpan struct {
	ID, Parent string
	Span
}

// TraceID returns the trace ID of a run: the bytes of its UID.
func TraceID(uid string) trace.TraceID {
	var id trace.TraceID
	b, err := hex.DecodeString(strings.ReplaceAll(uid, "-", ""))
	if err != nil || len(b) != len(id) {
		sum := sha256.Sum256([]byte(uid))
		b = sum[:]
	}
	copy(id[:], b)
	return id
}

// SpanID returns the span ID for a key of a span in a run.
func SpanID(uid, key string) trace.SpanID {
	var id trace.SpanID
	sum := sha256.Sum256([]byte(uid + "/" + key))
	copy(id[:], sum[:])
	return id
}

// Traceparent returns the W3C traceparent of a span key, so that the spans of a process become its children.
func Traceparent(uid, key string) string {
	return "00-" + TraceID(uid).String() + "-" + SpanID(uid, key).String() + "-01"
}

type fixedKey struct{}

type fixedIDs struct {
	trace trace.TraceID
	span  trace.SpanID
}

// ids gives the fixed IDs from EmitRun to the SDK, and random IDs to all other spans.
type ids struct{}

// IDGenerator returns the ID generator for the tracer provider. The span IDs of runs and nodes come from their UIDs,
// so a Pod can get the traceparent of its node before the node span exists.
func IDGenerator() sdktrace.IDGenerator { return ids{} }

func (ids) NewIDs(ctx context.Context) (trace.TraceID, trace.SpanID) {
	if f, ok := ctx.Value(fixedKey{}).(fixedIDs); ok {
		return f.trace, f.span
	}
	var t trace.TraceID
	_, _ = crand.Read(t[:])
	return t, randomSpanID()
}

func (ids) NewSpanID(ctx context.Context, _ trace.TraceID) trace.SpanID {
	if f, ok := ctx.Value(fixedKey{}).(fixedIDs); ok {
		return f.span
	}
	return randomSpanID()
}

func randomSpanID() trace.SpanID {
	var s trace.SpanID
	_, _ = crand.Read(s[:])
	return s
}

// EmitRun records the spans of a finished run: the run span, and one span per node under its parent node or the run.
// The ID of a span is its key in SpanID, so the controller can give a Pod the traceparent of its node in advance.
func EmitRun(ctx context.Context, tracer trace.Tracer, uid string, run NodeSpan, nodes []NodeSpan) {
	tid := TraceID(uid)
	emit(context.WithValue(ctx, fixedKey{}, fixedIDs{tid, SpanID(uid, run.ID)}), tracer, run.Span, trace.WithNewRoot())
	for _, n := range nodes {
		parent := cmp.Or(n.Parent, run.ID)
		pctx := trace.ContextWithSpanContext(ctx, trace.NewSpanContext(trace.SpanContextConfig{
			TraceID: tid, SpanID: SpanID(uid, parent), TraceFlags: trace.FlagsSampled,
		}))
		emit(context.WithValue(pctx, fixedKey{}, fixedIDs{tid, SpanID(uid, n.ID)}), tracer, n.Span)
	}
}

// emit starts and ends one span at the given times.
func emit(ctx context.Context, tracer trace.Tracer, s Span, opts ...trace.SpanStartOption) {
	attrs := make([]attribute.KeyValue, 0, len(s.Attributes))
	for k, v := range s.Attributes {
		if v == "" {
			continue
		}
		attrs = append(attrs, attribute.String(k, v))
	}
	opts = append(opts, trace.WithTimestamp(s.Start), trace.WithAttributes(attrs...))
	_, span := tracer.Start(ctx, s.Name, opts...)
	if s.Error != "" {
		span.SetStatus(codes.Error, s.Error)
	}
	span.End(trace.WithTimestamp(s.End))
}
