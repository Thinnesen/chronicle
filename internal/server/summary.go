package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
	chroniclev1 "github.com/Thinnesen/chronicle/gen/chronicle/v1"
	"github.com/Thinnesen/chronicle/internal/engine"
)

// summary returns the short form of a run.
func summary(wf *v1alpha1.Workflow) *chroniclev1.WorkflowSummary {
	s := &chroniclev1.WorkflowSummary{
		Namespace:  wf.Namespace,
		Name:       wf.Name,
		Uid:        string(wf.UID),
		Phase:      string(wf.Status.Phase),
		Message:    wf.Status.Message,
		CreatedAt:  timestamp(&wf.CreationTimestamp),
		StartedAt:  timestamp(wf.Status.StartedAt),
		FinishedAt: timestamp(wf.Status.FinishedAt),
		Labels:     wf.Labels,
	}
	if ref := wf.Spec.WorkflowTemplateRef; ref != nil {
		s.Template = ref.Name
	}
	s.NodesDone, s.NodesTotal = wf.Status.Progress()
	for _, id := range slices.Sorted(maps.Keys(wf.Status.Nodes)) {
		n := wf.Status.Nodes[id]
		switch {
		case n.Type == v1alpha1.NodeSuspend && n.Phase == v1alpha1.NodeRunning:
			s.ApprovalsWaiting++
		case n.Type == v1alpha1.NodePod && n.Phase == v1alpha1.NodePending && s.WaitingReason == "":
			s.WaitingReason = waitingReason(n.Message)
		}
	}
	return s
}

// reasonPrefix matches the Kubernetes reason at the start of a node message, for example "ImagePullBackOff: ...".
var reasonPrefix = regexp.MustCompile(`^([A-Z][A-Za-z]+): `)

// waitingReason returns the Kubernetes reason in the message of a pending Pod node, or "". A node that waits for a
// lock with a bad limit gives InvalidLockLimit: it waits until a user repairs the ConfigMap.
func waitingReason(message string) string {
	if m := reasonPrefix.FindStringSubmatch(message); m != nil {
		return m[1]
	}
	if rest, ok := strings.CutPrefix(message, engine.WaitingForLock+" "); ok && strings.Contains(rest, ": ") {
		return "InvalidLockLimit"
	}
	return ""
}

func timestamp(t *metav1.Time) *timestamppb.Timestamp {
	if t == nil || t.IsZero() {
		return nil
	}
	return timestamppb.New(t.Time)
}

// kinds knows the chronicle types, so that toStruct can set their kind.
var kinds = func() *runtime.Scheme {
	s := runtime.NewScheme()
	utilruntime.Must(v1alpha1.AddToScheme(s))
	return s
}()

// toStruct converts an object to a protobuf Struct through JSON. It sets the kind and the API version, which a typed
// read leaves empty. Then the object can go back to kubectl apply.
func toStruct(obj client.Object) (*structpb.Struct, error) {
	if gvk, err := apiutil.GVKForObject(obj, kinds); err == nil {
		obj.GetObjectKind().SetGroupVersionKind(gvk)
	}
	data, err := json.Marshal(obj)
	if err != nil {
		return nil, fmt.Errorf("encode the object: %w", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("decode the object: %w", err)
	}
	return structpb.NewStruct(m)
}

// fromStruct converts a protobuf Struct to a Workflow. Unknown fields are errors.
func fromStruct(s *structpb.Struct) (*v1alpha1.Workflow, error) {
	data, err := s.MarshalJSON()
	if err != nil {
		return nil, fmt.Errorf("encode the workflow: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var wf v1alpha1.Workflow
	if err := dec.Decode(&wf); err != nil {
		return nil, fmt.Errorf("the workflow is not valid: %w", err)
	}
	return &wf, nil
}
