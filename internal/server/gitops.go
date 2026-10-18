package server

import "sigs.k8s.io/controller-runtime/pkg/client"

// DescriptionAnnotation is a short description of a template or a CronWorkflow for the UI.
const DescriptionAnnotation = "chronicle.thinnesen.com/description"

// managedBy returns "Argo CD" or "Flux" when the labels or annotations show that a GitOps tool manages the object.
// Such an object is read-only in the UI: a change belongs in Git, or the tool reverts it.
func managedBy(obj client.Object) string {
	labels, annotations := obj.GetLabels(), obj.GetAnnotations()
	switch {
	case annotations["argocd.argoproj.io/tracking-id"] != "", labels["argocd.argoproj.io/instance"] != "":
		return "Argo CD"
	case labels["kustomize.toolkit.fluxcd.io/name"] != "", labels["helm.toolkit.fluxcd.io/name"] != "":
		return "Flux"
	}
	return ""
}
