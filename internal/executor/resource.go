package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/util/jsonpath"
)

// fieldManager is the field manager of the changes that a resource template makes.
const fieldManager = "chronicle"

// EnvResource holds the ResourceSpec as JSON.
const EnvResource = "CHRONICLE_RESOURCE"

// ResourceSpec is the work of a resource node.
type ResourceSpec struct {
	Action           string          `json:"action"`
	Manifest         json.RawMessage `json:"manifest"`
	SuccessCondition string          `json:"successCondition,omitempty"`
	FailureCondition string          `json:"failureCondition,omitempty"`
	// Namespace is the namespace of the workflow. It is the default for a namespaced object.
	Namespace string `json:"namespace"`
	// Owner is set when the Workflow must own the object.
	Owner   *metav1.OwnerReference `json:"owner,omitempty"`
	Outputs []JSONPathOutput       `json:"outputs,omitempty"`
}

// JSONPathOutput is an output parameter read from the object.
type JSONPathOutput struct {
	Name     string `json:"name"`
	JSONPath string `json:"jsonPath"`
}

// RunResource runs the action, waits for the conditions and reads the outputs.
func RunResource(ctx context.Context, cfg *rest.Config, spec ResourceSpec, poll time.Duration) Message {
	obj, res, err := prepare(cfg, spec)
	if err != nil {
		return Message{Error: err.Error()}
	}
	kind, name := obj.GetKind(), obj.GetName()
	if obj, err = act(ctx, res, spec, obj); err != nil {
		return Message{Error: fmt.Sprintf("%s %s %q: %v", spec.Action, kind, name, err)}
	}
	if spec.Action == "delete" {
		return Message{}
	}
	if obj, err = wait(ctx, res, spec, obj, poll); err != nil {
		return Message{Error: err.Error()}
	}
	params, err := readOutputs(spec.Outputs, obj)
	if err != nil {
		return Message{Error: err.Error()}
	}
	return Message{Parameters: params}
}

// prepare parses the manifest and finds the API resource of the object.
func prepare(cfg *rest.Config, spec ResourceSpec) (*unstructured.Unstructured, dynamic.ResourceInterface, error) {
	obj := &unstructured.Unstructured{}
	if err := obj.UnmarshalJSON(spec.Manifest); err != nil {
		return nil, nil, fmt.Errorf("bad manifest: %w", err)
	}
	dc, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("create the discovery client: %w", err)
	}
	mapper := restmapper.NewDeferredDiscoveryRESTMapper(memory.NewMemCacheClient(dc))
	gvk := obj.GroupVersionKind()
	mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return nil, nil, fmt.Errorf("find the API resource of %s: %w", gvk, err)
	}
	client, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("create the client: %w", err)
	}
	if mapping.Scope.Name() != meta.RESTScopeNameNamespace {
		if spec.Owner != nil {
			return nil, nil, fmt.Errorf("setOwnerReference needs an object in the workflow namespace")
		}
		return obj, client.Resource(mapping.Resource), nil
	}
	if obj.GetNamespace() == "" {
		obj.SetNamespace(spec.Namespace)
	}
	if spec.Owner != nil {
		if obj.GetNamespace() != spec.Namespace {
			return nil, nil, fmt.Errorf("setOwnerReference needs an object in the workflow namespace")
		}
		obj.SetOwnerReferences(append(obj.GetOwnerReferences(), *spec.Owner))
	}
	return obj, client.Resource(mapping.Resource).Namespace(obj.GetNamespace()), nil
}

func act(ctx context.Context, res dynamic.ResourceInterface, spec ResourceSpec, obj *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	switch spec.Action {
	case "create":
		return res.Create(ctx, obj, metav1.CreateOptions{FieldManager: fieldManager})
	case "apply":
		return res.Apply(ctx, obj.GetName(), obj, metav1.ApplyOptions{FieldManager: fieldManager, Force: true})
	case "patch":
		data, err := obj.MarshalJSON()
		if err != nil {
			return obj, err
		}
		return res.Patch(ctx, obj.GetName(), types.MergePatchType, data, metav1.PatchOptions{FieldManager: fieldManager})
	case "delete":
		err := res.Delete(ctx, obj.GetName(), metav1.DeleteOptions{PropagationPolicy: new(metav1.DeletePropagationBackground)})
		if apierrors.IsNotFound(err) {
			err = nil
		}
		return obj, err
	case "get":
		return res.Get(ctx, obj.GetName(), metav1.GetOptions{})
	}
	return obj, fmt.Errorf("unknown action %q", spec.Action)
}

// wait reads the object until the success or the failure condition matches.
func wait(ctx context.Context, res dynamic.ResourceInterface, spec ResourceSpec, obj *unstructured.Unstructured, poll time.Duration) (*unstructured.Unstructured, error) {
	if spec.SuccessCondition == "" && spec.FailureCondition == "" {
		return obj, nil
	}
	success, failure, err := parseConditions(spec)
	if err != nil {
		return obj, err
	}
	// Without a success condition, the node succeeds at once unless the failure condition matches.
	for {
		if failure != nil && failure.Match(obj) {
			return obj, fmt.Errorf("the failure condition %q matched", spec.FailureCondition)
		}
		if success == nil || success.Match(obj) {
			return obj, nil
		}
		select {
		case <-ctx.Done():
			return obj, ctx.Err()
		case <-time.After(poll):
		}
		if obj, err = res.Get(ctx, obj.GetName(), metav1.GetOptions{}); err != nil {
			return obj, fmt.Errorf("read the object: %w", err)
		}
	}
}

func parseConditions(spec ResourceSpec) (Condition, Condition, error) {
	var success, failure Condition
	var err error
	if spec.SuccessCondition != "" {
		if success, err = ParseCondition(spec.SuccessCondition); err != nil {
			return nil, nil, err
		}
	}
	if spec.FailureCondition != "" {
		if failure, err = ParseCondition(spec.FailureCondition); err != nil {
			return nil, nil, err
		}
	}
	return success, failure, nil
}

func readOutputs(outputs []JSONPathOutput, obj *unstructured.Unstructured) (map[string]string, error) {
	if len(outputs) == 0 {
		return nil, nil
	}
	params := map[string]string{}
	for _, o := range outputs {
		jp := jsonpath.New(o.Name)
		if err := jp.Parse(o.JSONPath); err != nil {
			return nil, fmt.Errorf("output parameter %q: %w", o.Name, err)
		}
		var buf bytes.Buffer
		if err := jp.Execute(&buf, obj.Object); err != nil {
			return nil, fmt.Errorf("output parameter %q: %w", o.Name, err)
		}
		params[o.Name] = strings.TrimSpace(buf.String())
	}
	return params, nil
}

// RunResourceMain runs the resource node of this Pod from the environment and returns the exit code.
func RunResourceMain(stderr io.Writer) int {
	var spec ResourceSpec
	if err := json.Unmarshal([]byte(os.Getenv(EnvResource)), &spec); err != nil {
		return fail(stderr, fmt.Errorf("bad %s: %w", EnvResource, err))
	}
	cfg, err := rest.InClusterConfig()
	if err != nil {
		return fail(stderr, fmt.Errorf("a resource template needs the service account token: %w", err))
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	msg := fitMessage(RunResource(ctx, cfg, spec, 5*time.Second))
	if err := writeMessage(msg); err != nil {
		_, _ = fmt.Fprintln(stderr, "chronicle-exec:", err)
	}
	if msg.Error != "" {
		_, _ = fmt.Fprintln(stderr, "chronicle-exec:", msg.Error)
		return 1
	}
	return 0
}
