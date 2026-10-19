package server

import (
	"context"
	"errors"
	"slices"
	"strings"

	"connectrpc.com/connect"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
	chroniclev1 "github.com/Thinnesen/chronicle/gen/chronicle/v1"
)

// templateService implements the TemplateService API.
type templateService struct {
	clients *clientFactory
}

func (s *templateService) ListTemplates(ctx context.Context, req *connect.Request[chroniclev1.ListTemplatesRequest]) (*connect.Response[chroniclev1.ListTemplatesResponse], error) {
	ns := req.Msg.Namespace
	if ns == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("give a namespace"))
	}
	c, err := s.clients.clientFor(ctx)
	if err != nil {
		return nil, err
	}
	var list v1alpha1.WorkflowTemplateList
	if err := c.List(ctx, &list, client.InNamespace(ns)); err != nil {
		return nil, toConnect(err)
	}
	resp := &chroniclev1.ListTemplatesResponse{}
	for i := range list.Items {
		t := &list.Items[i]
		resp.Templates = append(resp.Templates, templateSummary(t, &t.Spec, false))
	}
	// A user who cannot list cluster templates still sees the templates of the namespace.
	var cluster v1alpha1.ClusterWorkflowTemplateList
	if err := c.List(ctx, &cluster); err == nil {
		for i := range cluster.Items {
			t := &cluster.Items[i]
			resp.Templates = append(resp.Templates, templateSummary(t, &t.Spec, true))
		}
	} else if !apierrors.IsForbidden(err) {
		return nil, toConnect(err)
	}
	slices.SortFunc(resp.Templates, func(a, b *chroniclev1.TemplateSummary) int { return strings.Compare(a.Name, b.Name) })
	return connect.NewResponse(resp), nil
}

func (s *templateService) GetTemplate(ctx context.Context, req *connect.Request[chroniclev1.GetTemplateRequest]) (*connect.Response[chroniclev1.GetTemplateResponse], error) {
	m := req.Msg
	c, err := s.clients.clientFor(ctx)
	if err != nil {
		return nil, err
	}
	obj, spec, err := readTemplate(ctx, c, m.Namespace, m.Name, m.ClusterScope)
	if err != nil {
		return nil, toConnect(err)
	}
	obj.SetManagedFields(nil)
	st, err := toStruct(obj)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&chroniclev1.GetTemplateResponse{Summary: templateSummary(obj, spec, m.ClusterScope), Template: st}), nil
}

// readTemplate reads a WorkflowTemplate, or a ClusterWorkflowTemplate when cluster is true.
func readTemplate(ctx context.Context, c client.Client, namespace, name string, cluster bool) (client.Object, *v1alpha1.WorkflowTemplateSpec, error) {
	if cluster {
		t := &v1alpha1.ClusterWorkflowTemplate{}
		return t, &t.Spec, c.Get(ctx, client.ObjectKey{Name: name}, t)
	}
	t := &v1alpha1.WorkflowTemplate{}
	return t, &t.Spec, c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, t)
}

func templateSummary(obj client.Object, spec *v1alpha1.WorkflowTemplateSpec, cluster bool) *chroniclev1.TemplateSummary {
	created := obj.GetCreationTimestamp()
	s := &chroniclev1.TemplateSummary{
		Namespace: obj.GetNamespace(), Name: obj.GetName(), ClusterScope: cluster, Entrypoint: spec.Entrypoint,
		Description: obj.GetAnnotations()[DescriptionAnnotation], ManagedBy: managedBy(obj), CreatedAt: timestamp(&created),
	}
	for _, p := range spec.Arguments.Parameters {
		tp := &chroniclev1.TemplateParameter{Name: p.Name, Enum: p.Enum, Description: p.Description}
		if v := cmpPtr(p.Value, p.Default); v != nil {
			tp.Default, tp.HasDefault = *v, true
		}
		s.Parameters = append(s.Parameters, tp)
	}
	return s
}

func cmpPtr(a, b *string) *string {
	if a != nil {
		return a
	}
	return b
}
