package server

import (
	"context"
	"errors"
	"slices"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	chroniclev1 "github.com/Thinnesen/chronicle/gen/chronicle/v1"
)

// maxEvents limits the events of one Pod in an answer. Kubernetes counts a repeated event in one object.
const maxEvents = 100

// ListNodeEvents returns the events of the Pod of a node. The caller reads them with its own permissions, so it needs
// "list events" in the namespace.
func (s *workflowService) ListNodeEvents(ctx context.Context, req *connect.Request[chroniclev1.ListNodeEventsRequest]) (*connect.Response[chroniclev1.ListNodeEventsResponse], error) {
	m := req.Msg
	wf, err := s.get(ctx, m.Namespace, m.Workflow)
	if connect.CodeOf(err) == connect.CodeNotFound {
		// A run in the archive has no Pods and no events any more.
		return connect.NewResponse(&chroniclev1.ListNodeEventsResponse{}), nil
	}
	if err != nil {
		return nil, err
	}
	node, ok := wf.Status.Nodes[m.Node]
	if !ok {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("the run has no node with this ID"))
	}
	if node.PodName == "" {
		return connect.NewResponse(&chroniclev1.ListNodeEventsResponse{}), nil
	}
	c, err := s.client(ctx, m.Namespace)
	if err != nil {
		return nil, err
	}
	var list corev1.EventList
	if err := c.List(ctx, &list, client.InNamespace(m.Namespace),
		client.MatchingFields{"involvedObject.kind": "Pod", "involvedObject.name": node.PodName}); err != nil {
		return nil, toConnect(err)
	}
	slices.SortFunc(list.Items, func(a, b corev1.Event) int { return lastSeen(a).Compare(lastSeen(b)) })
	if len(list.Items) > maxEvents {
		list.Items = list.Items[len(list.Items)-maxEvents:]
	}
	out := make([]*chroniclev1.NodeEvent, 0, len(list.Items))
	for _, e := range list.Items {
		out = append(out, &chroniclev1.NodeEvent{Type: e.Type, Reason: e.Reason, Message: e.Message, Count: max(e.Count, 1),
			LastSeen: timestamppb.New(lastSeen(e))})
	}
	return connect.NewResponse(&chroniclev1.ListNodeEventsResponse{Events: out}), nil
}

// lastSeen is the latest time of an event. Newer events set only the event time, older ones only the timestamps.
func lastSeen(e corev1.Event) time.Time {
	switch {
	case !e.LastTimestamp.IsZero():
		return e.LastTimestamp.Time
	case !e.EventTime.IsZero():
		return e.EventTime.Time
	}
	return e.CreationTimestamp.Time
}
