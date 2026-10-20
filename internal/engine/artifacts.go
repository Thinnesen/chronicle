package engine

import (
	"fmt"
	"strings"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
	"github.com/Thinnesen/chronicle/internal/expr"
)

// resolveArtifacts returns the argument artifacts of a call: a repository key from from, or raw text or a URL.
// inputs are the inputs of the calling node: from "{{inputs.artifacts.x}}" passes on its raw text or URL too.
// An artifact that does not resolve is left out: it can be optional, and resolveInputs reports a missing artifact.
func resolveArtifacts(args []v1alpha1.Artifact, inputs *v1alpha1.Inputs, resolve expr.Resolver) map[string]v1alpha1.Artifact {
	out := map[string]v1alpha1.Artifact{}
	for _, a := range args {
		if in := passedInput(a.From, inputs); in != nil {
			out[a.Name] = v1alpha1.Artifact{Name: a.Name, Key: in.Key, Raw: in.Raw, HTTP: in.HTTP, Mode: in.Mode}
			continue
		}
		if a.Raw != nil || a.HTTP != nil {
			if s, err := withURL(a, resolve); err == nil {
				out[a.Name] = s
			}
			continue
		}
		if key, err := expr.Substitute(a.From, resolve); err == nil && key != "" {
			out[a.Name] = v1alpha1.Artifact{Name: a.Name, Key: key}
		}
	}
	return out
}

// passedInput returns the input artifact that from names as "{{inputs.artifacts.x}}", or nil.
func passedInput(from string, inputs *v1alpha1.Inputs) *v1alpha1.Artifact {
	name, ok := strings.CutPrefix(strings.TrimSpace(from), "{{inputs.artifacts.")
	if !ok || inputs == nil {
		return nil
	}
	name, ok = strings.CutSuffix(name, "}}")
	for i := range inputs.Artifacts {
		if ok && inputs.Artifacts[i].Name == strings.TrimSpace(name) {
			return &inputs.Artifacts[i]
		}
	}
	return nil
}

// inputArtifact gives an input artifact of a template its content. This is the argument, or else the raw text or the
// URL of the template. It returns false when there is none.
func inputArtifact(in v1alpha1.Artifact, args map[string]v1alpha1.Artifact, resolve expr.Resolver) (v1alpha1.Artifact, bool, error) {
	src, ok := args[in.Name]
	if !ok && in.Raw == nil && in.HTTP == nil {
		return v1alpha1.Artifact{}, false, nil
	}
	if !ok {
		var err error
		if src, err = withURL(in, resolve); err != nil {
			return v1alpha1.Artifact{}, false, fmt.Errorf("artifact %q: %w", in.Name, err)
		}
	}
	mode := src.Mode
	if mode == nil {
		mode = in.Mode
	}
	return v1alpha1.Artifact{Name: in.Name, Path: in.Path, Key: src.Key, Raw: src.Raw, HTTP: src.HTTP, Mode: mode}, true, nil
}

// withURL returns a raw or http artifact with the placeholders of its URL replaced. Raw text stays as it is.
func withURL(a v1alpha1.Artifact, resolve expr.Resolver) (v1alpha1.Artifact, error) {
	out := v1alpha1.Artifact{Name: a.Name, Raw: a.Raw, Mode: a.Mode}
	if a.HTTP != nil {
		url, err := expr.Substitute(a.HTTP.URL, resolve)
		if err != nil {
			return out, fmt.Errorf("url: %w", err)
		}
		out.HTTP = &v1alpha1.HTTPArtifact{URL: url}
	}
	return out, nil
}

// workflowArtifacts gives the artifacts of the workflow arguments to the entrypoint and the exit handler.
func (e *engine) workflowArtifacts() map[string]v1alpha1.Artifact {
	return resolveArtifacts(e.in.Spec.Arguments.Artifacts, nil, e.inputScope(nil))
}
