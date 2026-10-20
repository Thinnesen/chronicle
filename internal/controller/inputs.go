package controller

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/Thinnesen/chronicle/internal/executor"
)

const (
	// inputsVolume holds the input artifacts. It has no size limit, because an artifact can be large; the
	// ephemeral storage of the Pod limits it.
	inputsVolume = "chronicle-inputs"
	inputsDir    = "/var/run/chronicle-inputs"
)

// mountInputs lets the init container download the input artifacts into a volume. It mounts each input with subPath at
// its path in the main container. So a step without root can get an input at any path, as in Argo. A file and a
// directory both work. An artifact name has no slash, so a subPath stays in the volume.
func mountInputs(files *executor.ArtifactSpec, init, main *corev1.Container, volumes []corev1.Volume) []corev1.Volume {
	if len(files.Inputs) == 0 {
		return volumes
	}
	spec := *files
	spec.Outputs = nil
	if files.Repository != nil {
		// Only the main container archives its log.
		repo := *files.Repository
		repo.ArchiveLogs = false
		spec.Repository = &repo
	}
	spec.Inputs = make([]executor.File, 0, len(files.Inputs))
	for _, in := range files.Inputs {
		main.VolumeMounts = append(main.VolumeMounts, corev1.VolumeMount{Name: inputsVolume, MountPath: in.Path, SubPath: in.Name})
		in.Path = inputsDir + "/" + in.Name
		spec.Inputs = append(spec.Inputs, in)
	}
	init.Env = artifactEnv(&spec)
	init.VolumeMounts = append(init.VolumeMounts, corev1.VolumeMount{Name: inputsVolume, MountPath: inputsDir})
	// Unpacking fills the page cache, which counts against the limit.
	init.Resources.Limits[corev1.ResourceMemory] = resource.MustParse("256Mi")
	return append(volumes, corev1.Volume{Name: inputsVolume, EmptyDir: &corev1.EmptyDirVolumeSource{}})
}

// withoutInputs returns the artifact work of the main container: the init container has the inputs.
func withoutInputs(files *executor.ArtifactSpec) *executor.ArtifactSpec {
	spec := *files
	spec.Inputs = nil
	return &spec
}
