package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// VolumeClaimTemplate is a new, empty volume for each run. The steps of the run share it. The controller creates the
// claim <run>-<name> before the first Pod. A run that succeeds deletes its claims. A run that fails keeps them, so that
// a retry finds the files of the steps that succeeded.
type VolumeClaimTemplate struct {
	// name of the volume. A volumeMount uses it.
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	// +kubebuilder:validation:MaxLength=63
	// +required
	Name string `json:"name"`

	// storage is the size of the volume, for example 1Gi.
	// +required
	Storage resource.Quantity `json:"storage"`

	// storageClassName is the storage class. Without it, the cluster uses its default class.
	// +kubebuilder:validation:MaxLength=253
	// +optional
	StorageClassName *string `json:"storageClassName,omitempty"`

	// accessModes of the volume. The default is ReadWriteOnce. Then all Pods that mount it must run on one node.
	// +kubebuilder:validation:MaxItems=4
	// +optional
	AccessModes []corev1.PersistentVolumeAccessMode `json:"accessModes,omitempty"`
}

// Volume is a volume that steps can mount. Set one source. Each Pod gets its own emptyDir. Secret and configMap give
// the same access as env with secretKeyRef or configMapKeyRef.
// +kubebuilder:validation:XValidation:rule="[has(self.emptyDir), has(self.secret), has(self.configMap)].filter(x, x).size() == 1",message="set one of emptyDir, secret and configMap"
type Volume struct {
	// name of the volume. A volumeMount uses it.
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	// +kubebuilder:validation:MaxLength=63
	// +required
	Name string `json:"name"`

	// +optional
	EmptyDir *corev1.EmptyDirVolumeSource `json:"emptyDir,omitempty"`
	// +optional
	Secret *corev1.SecretVolumeSource `json:"secret,omitempty"`
	// +optional
	ConfigMap *corev1.ConfigMapVolumeSource `json:"configMap,omitempty"`
}

// VolumeMount mounts a volume or a volume claim template into the container of a step.
type VolumeMount struct {
	// name of a volume or a volume claim template of the workflow.
	// +kubebuilder:validation:MaxLength=63
	// +required
	Name string `json:"name"`

	// mountPath is the absolute path in the container.
	// +kubebuilder:validation:Pattern=`^/.+`
	// +kubebuilder:validation:MaxLength=4096
	// +required
	MountPath string `json:"mountPath"`

	// subPath mounts this directory of the volume, not its root.
	// +kubebuilder:validation:MaxLength=4096
	// +optional
	SubPath string `json:"subPath,omitempty"`

	// readOnly mounts the volume read-only.
	// +optional
	ReadOnly bool `json:"readOnly,omitempty"`
}
