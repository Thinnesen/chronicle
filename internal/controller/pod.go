package controller

import (
	"encoding/json"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
	"github.com/Thinnesen/chronicle/internal/artifact"
	"github.com/Thinnesen/chronicle/internal/engine"
	"github.com/Thinnesen/chronicle/internal/executor"
)

// The labels and annotations on workflow Pods.
const (
	LabelWorkflow    = "chronicle.thinnesen.com/workflow"
	LabelWorkflowUID = "chronicle.thinnesen.com/workflow-uid"
	LabelNodeID      = "chronicle.thinnesen.com/node-id"
	AnnotationNode   = "chronicle.thinnesen.com/node-name"
)

const (
	execVolume = "chronicle-exec"
	execDir    = "/var/run/chronicle"
	execPath   = execDir + "/exec"
	// executorBinary is the path of chronicle-exec in the executor image.
	executorBinary = "/chronicle-exec"
	// imageVolume holds the executor image, read-only, when the controller uses image volumes.
	imageVolume   = "chronicle-image"
	imageDir      = "/var/run/chronicle-image"
	imageExecPath = imageDir + executorBinary
	// mainContainer is the name of the container that runs the template.
	mainContainer = "main"
	// podUser is the default user of workflow containers. The restricted Pod security standard needs a non-root user.
	podUser = 65532
)

// buildPod returns the Pod for a CreatePod action. The Pod meets the "restricted" Pod security standard.
// files is the artifact work of the executor, or nil. With useImageVolume, the main container runs the executor from
// an image volume. Then a Pod without input artifacts needs no init container, which saves one container start.
func buildPod(wf *v1alpha1.Workflow, spec *v1alpha1.WorkflowTemplateSpec, a engine.CreatePod, executorImage string,
	useImageVolume bool, files *executor.ArtifactSpec) *corev1.Pod {
	labels := map[string]string{LabelWorkflowUID: string(wf.UID)}
	// A label value has at most 63 characters. The ID of the root node is the run name, which can be longer.
	if len(wf.Name) <= 63 {
		labels[LabelWorkflow] = wf.Name
	}
	if len(a.NodeID) <= 63 {
		labels[LabelNodeID] = a.NodeID
	}
	var main corev1.Container
	var mounts []v1alpha1.VolumeMount
	wrapped, automount := false, spec.ServiceAccountName != ""
	switch {
	case a.Template.Resource != nil:
		main, automount = resourceContainer(wf, a.Template, executorImage), true
	case a.Template.HTTP != nil:
		// An http request needs no access to the Kubernetes API.
		main, automount = httpContainer(a.Template.HTTP, executorImage), false
	case a.Template.Script != nil:
		main, wrapped = mainContainerFor(a.Template)
		mounts = a.Template.Script.VolumeMounts
	default:
		main, wrapped = mainContainerFor(a.Template)
		mounts = a.Template.Container.VolumeMounts
	}
	annotations := map[string]string{AnnotationNode: a.NodeName}
	initContainers := []corev1.Container(nil)
	volumes := []corev1.Volume(nil)
	if wrapped {
		// The volume holds the executor, the script and the log (up to 32 MiB) for the log archive.
		volumes = []corev1.Volume{{Name: execVolume, EmptyDir: &corev1.EmptyDirVolumeSource{
			SizeLimit: new(resource.MustParse("128Mi")),
		}}}
		if useImageVolume && (files == nil || len(files.Inputs) == 0) {
			volumes = append(volumes, corev1.Volume{Name: imageVolume, Image: &corev1.ImageVolumeSource{Reference: executorImage}})
			runFromImage(&main)
		} else {
			initContainers = []corev1.Container{initContainer(executorImage)}
		}
		if files != nil {
			main.Env = append(main.Env, artifactEnv(withoutInputs(files))...)
			annotations[AnnotationArtifactPrefix] = files.Prefix
			if files.Repository != nil && files.Repository.ArchiveLogs {
				annotations[AnnotationLogKey] = artifact.LogKey(files.Prefix)
			}
			if len(initContainers) > 0 {
				volumes = mountInputs(files, &initContainers[0], &main, volumes)
			}
		}
	}
	volumes = mountVolumes(wf, spec, mounts, &main, volumes)
	return &corev1.Pod{
		Name:            a.PodName,
		Namespace:       wf.Namespace,
		Labels:          labels,
		Annotations:     annotations,
		OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(wf, v1alpha1.GroupVersion.WithKind("Workflow"))},
		Spec: corev1.PodSpec{
			RestartPolicy:      corev1.RestartPolicyNever,
			ServiceAccountName: spec.ServiceAccountName,
			// A Pod gets an API token only for a resource template. It also gets one when the workflow names a service account,
			// except for an http template.
			AutomountServiceAccountToken: new(automount),
			EnableServiceLinks:           new(false),
			SecurityContext: &corev1.PodSecurityContext{
				RunAsNonRoot:   new(true),
				RunAsUser:      ptr.To[int64](podUser),
				RunAsGroup:     ptr.To[int64](podUser),
				FSGroup:        ptr.To[int64](podUser),
				SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
			},
			InitContainers: initContainers,
			Containers:     []corev1.Container{main},
			Volumes:        volumes,
		},
	}
}

// usesExecutor tells if the executor wraps the main container of a template.
func usesExecutor(t *v1alpha1.Template) bool {
	return t.Script != nil || t.Container != nil && len(t.Container.Command) > 0
}

// mainContainerFor returns the main container. When the template has a command, the executor wraps it,
// so that the Pod can report outputs. It returns false when the container runs without the executor.
func mainContainerFor(t *v1alpha1.Template) (corev1.Container, bool) {
	ct := t.Container
	command := []string(nil)
	env := []corev1.EnvVar(nil)
	if t.Script != nil {
		ct = &t.Script.ContainerTemplate
		command = []string{"sh"}
		for i, part := range executor.SplitScript(t.Script.Source) {
			env = append(env, corev1.EnvVar{Name: executor.ScriptEnvName(i), Value: part})
		}
	}
	if len(ct.Command) > 0 {
		command = ct.Command
	}
	c := corev1.Container{
		Name:                     mainContainer,
		Image:                    ct.Image,
		ImagePullPolicy:          ct.ImagePullPolicy,
		WorkingDir:               ct.WorkingDir,
		Env:                      append(env, ct.Env...),
		EnvFrom:                  ct.EnvFrom,
		Resources:                ct.Resources,
		TerminationMessagePolicy: corev1.TerminationMessageReadFile,
		SecurityContext:          restricted(),
	}
	if command == nil {
		c.Args = ct.Args
		return c, false
	}
	if outs := outputFiles(t); outs != "" {
		c.Env = append(c.Env, corev1.EnvVar{Name: executor.EnvOutputs, Value: outs})
	}
	c.Command = append(append([]string{execPath, "run", "--"}, command...), ct.Args...)
	c.VolumeMounts = []corev1.VolumeMount{{Name: execVolume, MountPath: execDir}}
	return c, true
}

// resourceContainer runs "chronicle-exec resource" with the service account of the workflow.
func resourceContainer(wf *v1alpha1.Workflow, t *v1alpha1.Template, executorImage string) corev1.Container {
	r := t.Resource
	spec := executor.ResourceSpec{
		Action: string(r.Action), Manifest: json.RawMessage(r.Manifest), Namespace: wf.Namespace,
		SuccessCondition: r.SuccessCondition, FailureCondition: r.FailureCondition,
	}
	if r.SetOwnerReference {
		spec.Owner = metav1.NewControllerRef(wf, v1alpha1.GroupVersion.WithKind("Workflow"))
	}
	for _, p := range t.Outputs.Parameters {
		spec.Outputs = append(spec.Outputs, executor.JSONPathOutput{Name: p.Name, JSONPath: p.ValueFrom.JSONPath})
	}
	data, _ := json.Marshal(spec)
	return corev1.Container{
		Name:                     mainContainer,
		Image:                    executorImage,
		Command:                  []string{executorBinary, "resource"},
		Env:                      []corev1.EnvVar{{Name: executor.EnvResource, Value: string(data)}},
		TerminationMessagePolicy: corev1.TerminationMessageReadFile,
		SecurityContext:          readOnly(restricted()),
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("10m"), corev1.ResourceMemory: resource.MustParse("32Mi")},
			Limits:   corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("128Mi")},
		},
	}
}

func outputFiles(t *v1alpha1.Template) string {
	var outs []executor.Output
	for _, p := range t.Outputs.Parameters {
		if p.ValueFrom != nil && p.ValueFrom.Path != "" {
			outs = append(outs, executor.Output{Name: p.Name, Path: p.ValueFrom.Path})
		}
	}
	if len(outs) == 0 {
		return ""
	}
	b, _ := json.Marshal(outs)
	return string(b)
}

// runFromImage makes the main container run the executor of the image volume. The executor keeps its work files,
// such as the log for the archive, in the shared volume.
func runFromImage(main *corev1.Container) {
	main.Command[0] = imageExecPath
	main.VolumeMounts = append(main.VolumeMounts, corev1.VolumeMount{Name: imageVolume, MountPath: imageDir, ReadOnly: true})
	main.Env = append(main.Env, corev1.EnvVar{Name: executor.EnvWorkDir, Value: execDir})
}

func initContainer(image string) corev1.Container {
	return corev1.Container{
		Name:            "init",
		Image:           image,
		Command:         []string{executorBinary, "install", execPath},
		SecurityContext: readOnly(restricted()),
		VolumeMounts:    []corev1.VolumeMount{{Name: execVolume, MountPath: execDir}},
		// The copy of the executor (about 40 MB) fills the page cache, which counts against the limit.
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("10m"), corev1.ResourceMemory: resource.MustParse("16Mi")},
			Limits:   corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("128Mi")},
		},
	}
}

// readOnly adds a read-only root file system. Only the containers of the chronicle image get it. The image of a user
// step often writes to its root file system (see docs/DECISIONS.md).
func readOnly(sc *corev1.SecurityContext) *corev1.SecurityContext {
	sc.ReadOnlyRootFilesystem = new(true)
	return sc
}

func restricted() *corev1.SecurityContext {
	return &corev1.SecurityContext{
		AllowPrivilegeEscalation: new(false),
		Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
	}
}
