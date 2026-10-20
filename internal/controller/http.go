package controller

import (
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
	"github.com/Thinnesen/chronicle/internal/executor"
)

// httpContainer runs "chronicle-exec http". A header value from a Secret comes in through an environment variable. So
// the controller never reads the Secret, and the value is not in the Pod spec.
func httpContainer(h *v1alpha1.HTTPTemplate, executorImage string) corev1.Container {
	spec := executor.HTTPSpec{Method: string(h.Method), URL: h.URL, Body: h.Body}
	env := []corev1.EnvVar{{Name: executor.EnvHTTP}}
	for i, hd := range h.Headers {
		if hd.ValueFrom == nil {
			spec.Headers = append(spec.Headers, executor.HTTPHeaderSpec{Name: hd.Name, Value: *hd.Value})
			continue
		}
		name := fmt.Sprintf("CHRONICLE_HEADER_%d", i)
		spec.Headers = append(spec.Headers, executor.HTTPHeaderSpec{Name: hd.Name, Env: name})
		env = append(env, corev1.EnvVar{Name: name, ValueFrom: &corev1.EnvVarSource{SecretKeyRef: hd.ValueFrom.SecretKeyRef.DeepCopy()}})
	}
	data, _ := json.Marshal(spec) // Strings always encode.
	env[0].Value = string(data)
	return corev1.Container{
		Name:                     mainContainer,
		Image:                    executorImage,
		Command:                  []string{executorBinary, "http"},
		Env:                      env,
		TerminationMessagePolicy: corev1.TerminationMessageReadFile,
		SecurityContext:          readOnly(restricted()),
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("10m"), corev1.ResourceMemory: resource.MustParse("32Mi")},
			Limits:   corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("128Mi")},
		},
	}
}
