package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	chroniclev1alpha1 "github.com/Thinnesen/chronicle/api/v1alpha1"
	"github.com/Thinnesen/chronicle/internal/artifact"
	"github.com/Thinnesen/chronicle/internal/engine"
	"github.com/Thinnesen/chronicle/internal/executor"
)

// AnnotationArtifactPrefix is the key prefix of the artifacts and the log of a Pod.
const AnnotationArtifactPrefix = "chronicle.thinnesen.com/artifact-prefix"

// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get

// artifactSpec returns what the executor of a new Pod downloads and saves, or nil when it has nothing to do.
// The controller never reads the Secret: the Pod gets the credentials from it directly.
func (r *WorkflowReconciler) artifactSpec(ctx context.Context, wf *chroniclev1alpha1.Workflow, a engine.CreatePod) (*executor.ArtifactSpec, error) {
	spec := &executor.ArtifactSpec{}
	if a.Inputs != nil {
		for _, in := range a.Inputs.Artifacts {
			f := executor.File{Name: in.Name, Path: in.Path, Key: in.Key, Mode: in.Mode}
			if in.Raw != nil {
				f.Raw = &in.Raw.Data
			}
			if in.HTTP != nil {
				f.URL = in.HTTP.URL
			}
			spec.Inputs = append(spec.Inputs, f)
		}
	}
	for _, out := range a.Template.Outputs.Artifacts {
		spec.Outputs = append(spec.Outputs, executor.File{Name: out.Name, Path: out.Path, Optional: out.Optional})
	}
	repo, err := r.repository(ctx, wf.Namespace)
	var bad badRepositoryError
	switch {
	case errors.As(err, &bad) && spec.NeedsRepository():
		// The executor reports the problem in the node.
		spec.Error = err.Error()
		return spec, nil
	case errors.As(err, &bad):
		logf.FromContext(ctx).Info("The log archive is off", "reason", err.Error())
	case err != nil:
		return nil, err
	}
	if repo != nil {
		spec.Repository = repo
		spec.Prefix = repo.Prefix(wf.Namespace, string(wf.UID), a.PodName)
	}
	if len(spec.Inputs) == 0 && len(spec.Outputs) == 0 && (repo == nil || !repo.ArchiveLogs) {
		return nil, nil
	}
	return spec, nil
}

// repository reads the artifact repository of a namespace. It returns nil when the namespace has none.
// A bad ConfigMap is a badRepositoryError.
func (r *WorkflowReconciler) repository(ctx context.Context, namespace string) (*artifact.Repository, error) {
	reader := r.APIReader
	if reader == nil {
		reader = r.Client
	}
	var cm corev1.ConfigMap
	err := reader.Get(ctx, client.ObjectKey{Namespace: namespace, Name: artifact.Name}, &cm)
	if apierrors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read the artifact repository: %w", err)
	}
	repo, err := artifact.FromConfigMap(cm.Data)
	if err != nil {
		return nil, badRepositoryError{err}
	}
	return &repo, nil
}

// badRepositoryError is a ConfigMap with bad data.
type badRepositoryError struct{ error }

// artifactEnv returns the environment of the executor for artifacts.
func artifactEnv(spec *executor.ArtifactSpec) []corev1.EnvVar {
	data, _ := json.Marshal(spec)
	secret := func(name, key string) corev1.EnvVar {
		return corev1.EnvVar{Name: name, ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
			Name: artifact.Name, Key: key, Optional: new(true),
		}}}
	}
	return []corev1.EnvVar{
		{Name: executor.EnvArtifacts, Value: string(data)},
		secret(executor.EnvAccessKey, artifact.SecretAccessKey),
		secret(executor.EnvSecretKey, artifact.SecretSecretKey),
	}
}
