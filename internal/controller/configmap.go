package controller

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Thinnesen/chronicle/internal/resolve"
)

// LabelParameters marks a ConfigMap whose values workflows may read as parameters. Without it, a user who may
// create Workflows could read any ConfigMap of the namespace through a run.
const LabelParameters = "chronicle.thinnesen.com/parameters"

// configMapGetter reads parameter values from ConfigMaps in one namespace.
type configMapGetter struct {
	client.Reader
	namespace string
}

func (g configMapGetter) ConfigMapValue(ctx context.Context, name, key string) (string, bool, error) {
	var cm corev1.ConfigMap
	if err := g.Get(ctx, client.ObjectKey{Namespace: g.namespace, Name: name}, &cm); err != nil {
		return "", false, fmt.Errorf("read ConfigMap %q: %w", name, err)
	}
	if cm.Labels[LabelParameters] != "true" {
		return "", false, fmt.Errorf("ConfigMap %q has no label %s: \"true\"; add it to share its values with workflows: %w",
			name, LabelParameters, resolve.ErrInvalid)
	}
	v, ok := cm.Data[key]
	return v, ok, nil
}
