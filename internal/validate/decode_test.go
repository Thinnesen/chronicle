package validate

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
)

func TestStrict(t *testing.T) {
	tests := []struct {
		json, want string
	}{
		{`{"spec":{"entrypoint":"c","templates":[{"name":"c","timeout":"5x"}]}}`, `spec.templates[0].timeout: time: unknown unit "x" in duration "5x"`},
		{`{"metadata":{"name":"soon"},"spec":{"templates":[{"name":"c","suspend":{"duration":"soon"}}]}}`, `spec.templates[0].suspend.duration: time: invalid duration "soon"`},
		{`{"spec":{"entrypoint":"c","nope":1}}`, `unknown field "nope"`},
		{`{"spec":{"templates":[{"name":"c","retryStrategy":{"backoff":{"duration":"-5s"}}}]}}`, `spec.templates[0].retryStrategy.backoff.duration: a duration cannot be negative`},
		{`{"spec":{"startAt":"tomorrow"}}`, `spec.startAt: parsing time "tomorrow"`},
		{`{"spec":{"entrypoint":"c"}}`, ""},
	}
	for _, tt := range tests {
		var wf v1alpha1.Workflow
		err := Strict([]byte(tt.json), &wf)
		if tt.want == "" && err != nil || tt.want != "" && (err == nil || !strings.Contains(err.Error(), tt.want)) {
			t.Errorf("Strict(%s) = %v, want %q", tt.json, err, tt.want)
		}
	}
}

// TestDurationFields checks that durationFields has the JSON name of each duration field of the API.
func TestDurationFields(t *testing.T) {
	duration := reflect.TypeFor[metav1.Duration]()
	seen := map[reflect.Type]bool{}
	var walk func(reflect.Type)
	walk = func(typ reflect.Type) {
		for typ.Kind() == reflect.Pointer || typ.Kind() == reflect.Slice || typ.Kind() == reflect.Map {
			typ = typ.Elem()
		}
		if typ.Kind() != reflect.Struct || seen[typ] || typ.PkgPath() != reflect.TypeFor[v1alpha1.Workflow]().PkgPath() {
			return
		}
		seen[typ] = true
		for f := range typ.Fields() {
			name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
			ft := f.Type
			if ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if ft == duration && !slices.Contains(durationFields, name) {
				t.Errorf("durationFields has no %q (%s.%s)", name, typ.Name(), f.Name)
			}
			walk(f.Type)
		}
	}
	walk(reflect.TypeFor[v1alpha1.WorkflowSpec]())
	walk(reflect.TypeFor[v1alpha1.CronWorkflowSpec]())
}
