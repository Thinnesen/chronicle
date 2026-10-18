package executor

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/yaml"
)

var cfg *rest.Config

func TestMain(m *testing.M) {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		os.Exit(m.Run()) // the envtest cases skip themselves
	}
	env := &envtest.Environment{}
	var err error
	if cfg, err = env.Start(); err != nil {
		panic(err)
	}
	code := m.Run()
	_ = env.Stop()
	os.Exit(code)
}

func resourceSpec(t *testing.T, action, manifest string) ResourceSpec {
	t.Helper()
	data, err := yaml.YAMLToJSON([]byte(manifest))
	if err != nil {
		t.Fatal(err)
	}
	return ResourceSpec{Action: action, Manifest: data, Namespace: "default"}
}

func TestRunResource(t *testing.T) {
	if cfg == nil {
		t.Skip("envtest is not set up: run make test")
	}
	ctx := context.Background()
	cm := "apiVersion: v1\nkind: ConfigMap\nmetadata: {name: res}\ndata: {state: done, count: '3'}"

	spec := resourceSpec(t, "create", cm)
	spec.SuccessCondition = "data.state == done, data.count >= 3"
	spec.Outputs = []JSONPathOutput{{Name: "ns", JSONPath: "{.metadata.namespace}"}}
	if msg := RunResource(ctx, cfg, spec, time.Millisecond); msg.Error != "" || msg.Parameters["ns"] != "default" {
		t.Fatalf("create: %+v", msg)
	}

	spec = resourceSpec(t, "apply", strings.Replace(cm, "count: '3'", "count: '4'", 1))
	spec.FailureCondition = "data.count > 3"
	if msg := RunResource(ctx, cfg, spec, time.Millisecond); !strings.Contains(msg.Error, "failure condition") {
		t.Fatalf("apply with failure: %+v", msg)
	}

	spec = resourceSpec(t, "patch", "apiVersion: v1\nkind: ConfigMap\nmetadata: {name: res}\ndata: {state: patched}")
	spec.Owner = &metav1.OwnerReference{APIVersion: "v1", Kind: "ConfigMap", Name: "owner", UID: "1234"}
	if msg := RunResource(ctx, cfg, spec, time.Millisecond); msg.Error != "" {
		t.Fatalf("patch: %+v", msg)
	}

	spec = resourceSpec(t, "get", "apiVersion: v1\nkind: ConfigMap\nmetadata: {name: res}")
	spec.Outputs = []JSONPathOutput{{Name: "state", JSONPath: "{.data.state}"}}
	if msg := RunResource(ctx, cfg, spec, time.Millisecond); msg.Parameters["state"] != "patched" {
		t.Fatalf("get: %+v", msg)
	}

	for range 2 { // a second delete finds nothing and still succeeds
		if msg := RunResource(ctx, cfg, resourceSpec(t, "delete", cm), time.Millisecond); msg.Error != "" {
			t.Fatalf("delete: %+v", msg)
		}
	}

	spec = resourceSpec(t, "create", "apiVersion: v1\nkind: Namespace\nmetadata: {name: owned}")
	spec.Owner = &metav1.OwnerReference{APIVersion: "v1", Kind: "ConfigMap", Name: "owner", UID: "1234"}
	if msg := RunResource(ctx, cfg, spec, time.Millisecond); !strings.Contains(msg.Error, "setOwnerReference") {
		t.Fatalf("owner of a cluster object: %+v", msg)
	}

	spec = resourceSpec(t, "get", "apiVersion: v1\nkind: ConfigMap\nmetadata: {name: missing}")
	if msg := RunResource(ctx, cfg, spec, time.Millisecond); !strings.Contains(msg.Error, "not found") {
		t.Fatalf("get a missing object: %+v", msg)
	}
}

func TestCondition(t *testing.T) {
	var obj unstructured.Unstructured
	_ = json.Unmarshal([]byte(`{"status":{"succeeded":2,"phase":"Done"}}`), &obj.Object)
	tests := []struct {
		cond string
		want bool
	}{
		{"status.succeeded > 0", true},
		{"status.succeeded >= 3", false},
		{"status.succeeded == 2, status.phase = Done", true},
		{"status.phase != Done", false},
		{"status.missing != x", true},
		{"status.missing == x", false},
		{"status.phase > a", false},
		{"status.succeeded < 3", true},
		{"status.succeeded <= 1", false},
	}
	for _, tt := range tests {
		c, err := ParseCondition(tt.cond)
		if err != nil {
			t.Fatal(err)
		}
		if got := c.Match(&obj); got != tt.want {
			t.Errorf("%q = %v, want %v", tt.cond, got, tt.want)
		}
	}
	for _, bad := range []string{"status", "== 1", "a ==", "a,"} {
		if _, err := ParseCondition(bad); err == nil {
			t.Errorf("ParseCondition(%q) returned no error", bad)
		}
	}
}
