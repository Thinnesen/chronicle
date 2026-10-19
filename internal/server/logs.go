package server

import (
	"bufio"
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
	chroniclev1 "github.com/Thinnesen/chronicle/gen/chronicle/v1"
)

// maxLogLine is the longest log line. A longer line is cut.
const maxLogLine = 256 << 10

// podLogs opens the log of the main container of a Pod as the caller.
type podLogs func(ctx context.Context, namespace, pod string, opts *corev1.PodLogOptions) (io.ReadCloser, error)

// logService implements the LogService API.
type logService struct {
	workflows *workflowService
	open      podLogs
	stores    ArtifactStores
}

func (s *logService) StreamLogs(ctx context.Context, req *connect.Request[chroniclev1.StreamLogsRequest], stream *connect.ServerStream[chroniclev1.StreamLogsResponse]) error {
	m := req.Msg
	wf, _, err := s.workflows.getAny(ctx, m.Namespace, m.Workflow)
	if err != nil {
		return err
	}
	nodes := podNodes(wf, m.Node)
	if len(nodes) == 0 && m.Node != "" {
		return connect.NewError(connect.CodeNotFound, errors.New("the run has no Pod node with this ID or name"))
	}
	opts := &corev1.PodLogOptions{Container: "main", Follow: m.Follow, Timestamps: true}
	if m.TailLines > 0 {
		opts.TailLines = &m.TailLines
	}
	var mu sync.Mutex
	send := func(r *chroniclev1.StreamLogsResponse) error {
		mu.Lock()
		defer mu.Unlock()
		return stream.Send(r)
	}
	errs := make([]error, len(nodes))
	var wg sync.WaitGroup
	for i, n := range nodes {
		wg.Go(func() { errs[i] = s.streamNode(ctx, m.Namespace, n, opts, send) })
	}
	wg.Wait()
	return toConnect(errors.Join(errs...))
}

func (s *logService) streamNode(ctx context.Context, namespace string, n v1alpha1.NodeStatus, opts *corev1.PodLogOptions, send func(*chroniclev1.StreamLogsResponse) error) error {
	rc, err := s.open(ctx, namespace, n.PodName, opts)
	if apierrors.IsNotFound(err) && n.LogKey != "" {
		return s.streamArchived(ctx, namespace, n, opts.TailLines, send)
	}
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()
	sc := bufio.NewScanner(rc)
	sc.Buffer(make([]byte, 64<<10), maxLogLine)
	for sc.Scan() {
		ts, line := splitTimestamp(sc.Text())
		if err := send(&chroniclev1.StreamLogsResponse{NodeId: n.ID, NodeName: n.Name, Time: ts, Line: line}); err != nil {
			return err
		}
	}
	if err := sc.Err(); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

// streamArchived sends the archived log of a node after an access review of the caller for "get pods/log".
func (s *logService) streamArchived(ctx context.Context, namespace string, n v1alpha1.NodeStatus, tail *int64, send func(*chroniclev1.StreamLogsResponse) error) error {
	c, err := s.workflows.client(ctx, namespace)
	if err != nil {
		return err
	}
	if !canReadLogs(ctx, c, namespace) {
		return connect.NewError(connect.CodePermissionDenied, errors.New("you cannot read logs in this namespace"))
	}
	rc, _, err := openStored(ctx, s.stores, namespace, n.LogKey)
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()
	var lines []string
	sc := bufio.NewScanner(rc)
	sc.Buffer(make([]byte, 64<<10), maxLogLine)
	for sc.Scan() {
		lines = append(lines, sc.Text())
		if tail != nil && int64(len(lines)) > *tail {
			lines = lines[1:]
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	for _, line := range lines {
		if err := send(&chroniclev1.StreamLogsResponse{NodeId: n.ID, NodeName: n.Name, Line: line, Archived: true}); err != nil {
			return err
		}
	}
	return nil
}

// podNodes returns the Pod nodes of a run, or the one node with the ID or name, sorted by name.
func podNodes(wf *v1alpha1.Workflow, node string) []v1alpha1.NodeStatus {
	var out []v1alpha1.NodeStatus
	for _, n := range wf.Status.Nodes {
		if n.PodName != "" && (node == "" || n.ID == node || n.Name == node) {
			out = append(out, n)
		}
	}
	slices.SortFunc(out, func(a, b v1alpha1.NodeStatus) int { return strings.Compare(a.Name, b.Name) })
	return out
}

// splitTimestamp splits the RFC 3339 time that Kubernetes puts before each line with Timestamps.
func splitTimestamp(s string) (*timestamppb.Timestamp, string) {
	prefix, rest, ok := strings.Cut(s, " ")
	if !ok {
		return nil, s
	}
	t, err := time.Parse(time.RFC3339Nano, prefix)
	if err != nil {
		return nil, s
	}
	return timestamppb.New(t), rest
}
