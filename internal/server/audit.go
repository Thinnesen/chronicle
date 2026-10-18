package server

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
	chroniclev1 "github.com/Thinnesen/chronicle/gen/chronicle/v1"
	"github.com/Thinnesen/chronicle/internal/auth"
)

// auditEntry describes one state-changing call.
type auditEntry struct {
	Action    string
	User      string
	Groups    []string
	Namespace string
	Name      string
	Detail    string
	Code      string // "ok" or the Connect error code
}

// auditor writes each state-changing call to the audit log and as a Kubernetes Event on the Workflow.
type auditor struct {
	log logr.Logger
	// events creates Events with the permissions of the server.
	events client.Client
}

// interceptor audits each call that is not marked as free of side effects in the protobuf API.
func (a *auditor) interceptor() connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			resp, err := next(ctx, req)
			if req.Spec().IdempotencyLevel != connect.IdempotencyNoSideEffects {
				a.record(ctx, newAuditEntry(ctx, req, resp, err))
			}
			return resp, err
		}
	}
}

func newAuditEntry(ctx context.Context, req connect.AnyRequest, resp connect.AnyResponse, err error) auditEntry {
	proc := req.Spec().Procedure
	e := auditEntry{Action: proc[strings.LastIndex(proc, "/")+1:], Code: "ok"}
	if id := auth.IdentityFrom(ctx); id != nil {
		e.User, e.Groups = id.User, id.Groups
	}
	if m, ok := req.Any().(interface{ GetNamespace() string }); ok {
		e.Namespace = m.GetNamespace()
	}
	if m, ok := req.Any().(interface{ GetName() string }); ok {
		e.Name = m.GetName()
	}
	if m, ok := req.Any().(*chroniclev1.ApproveNodeRequest); ok {
		e.Detail = fmt.Sprintf("node %q, approve %t", m.Node, m.Approve)
	}
	if resp != nil {
		if m, ok := resp.Any().(interface {
			GetSummary() *chroniclev1.WorkflowSummary
		}); ok && m.GetSummary() != nil {
			e.Name = m.GetSummary().Name
		}
	}
	if err != nil {
		e.Code = connect.CodeOf(err).String()
	}
	return e
}

func (a *auditor) record(ctx context.Context, e auditEntry) {
	a.log.Info("audit", "action", e.Action, "user", e.User, "groups", e.Groups, "namespace", e.Namespace,
		"name", e.Name, "detail", e.Detail, "result", e.Code)
	if e.Code != "ok" || e.Name == "" {
		return
	}
	if err := a.event(ctx, e); err != nil {
		a.log.Error(err, "Failed to write the audit event", "action", e.Action, "namespace", e.Namespace, "name", e.Name)
	}
}

// event writes a Kubernetes Event on the Workflow, so that `kubectl describe` shows who changed the run.
func (a *auditor) event(ctx context.Context, e auditEntry) error {
	now := metav1.NewTime(time.Now())
	msg := fmt.Sprintf("%s by %s", e.Action, e.User)
	if e.Detail != "" {
		msg += ": " + e.Detail
	}
	ev := &corev1.Event{
		GenerateName: e.Name + "-",
		Namespace:    e.Namespace,
		InvolvedObject: corev1.ObjectReference{
			APIVersion: v1alpha1.GroupVersion.String(), Kind: "Workflow", Namespace: e.Namespace, Name: e.Name,
		},
		Reason: e.Action, Message: msg, Type: corev1.EventTypeNormal,
		Source:         corev1.EventSource{Component: "chronicle-server"},
		FirstTimestamp: now, LastTimestamp: now, Count: 1,
	}
	if err := a.events.Create(ctx, ev); err != nil {
		return errors.Join(errors.New("create the event"), err)
	}
	return nil
}
