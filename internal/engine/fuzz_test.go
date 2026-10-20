package engine

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
)

var fuzzResults = []string{"", ".Succeeded", ".Failed", ".Errored", ".Omitted", ".Skipped"}
var fuzzOutcomes = []v1alpha1.NodePhase{succeeded, succeeded, failed, errored}

// fuzzDAG builds a random DAG and Pod outcomes from data. Each task depends only on earlier tasks.
func fuzzDAG(data []byte) (string, map[string]v1alpha1.NodePhase, map[string][]string, int) {
	next := func() int {
		if len(data) == 0 {
			return 0
		}
		b := int(data[0])
		data = data[1:]
		return b
	}
	n := 1 + next()%8
	var tasks strings.Builder
	outcomes := map[string]v1alpha1.NodePhase{}
	deps := map[string][]string{}
	for i := range n {
		name := fmt.Sprintf("t%d", i)
		mask, useDepends := next(), next()%2 == 1
		var refs []string
		for j := range i {
			if mask&(1<<j) != 0 {
				deps[name] = append(deps[name], fmt.Sprintf("t%d", j))
				refs = append(refs, fmt.Sprintf("t%d%s", j, fuzzResults[next()%len(fuzzResults)]))
			}
		}
		fmt.Fprintf(&tasks, "    - {name: %s, template: c", name)
		switch {
		case len(refs) > 0 && useDepends:
			op := []string{" && ", " || "}[next()%2]
			fmt.Fprintf(&tasks, ", depends: %q", strings.Join(refs, op))
		case len(refs) > 0:
			fmt.Fprintf(&tasks, ", dependencies: [%s]", strings.Join(deps[name], ", "))
		}
		tasks.WriteString("}\n")
		outcomes["wf."+name] = fuzzOutcomes[next()%len(fuzzOutcomes)]
	}
	ff := []string{"true", "false"}[next()%2]
	// limit is the parallelism of the DAG; 0 is none.
	limit, parallelism := next()%4, ""
	if limit > 0 {
		parallelism = fmt.Sprintf("  parallelism: %d\n", limit)
	}
	spec := "entrypoint: main\ntemplates:\n- name: main\n" + parallelism + "  dag:\n    failFast: " + ff + "\n    tasks:\n" + tasks.String() + pod
	return spec, outcomes, deps, limit
}

func FuzzEngine(f *testing.F) {
	f.Add([]byte{3, 0, 0, 1, 1, 0, 0, 3, 1, 2, 0, 0})
	f.Add([]byte{7, 255, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16})
	f.Add([]byte{5, 0, 0, 2, 1, 1, 2, 3, 0, 1, 7, 1, 1, 0, 2, 0, 15, 0, 3, 1})
	f.Fuzz(func(t *testing.T, data []byte) {
		spec, outcomes, deps, limit := fuzzDAG(data)
		s := newSim(t, spec, outcomes)
		var st v1alpha1.WorkflowStatus
		for i := range 100 {
			r := s.step(i)
			checkStartOrder(t, r, deps)
			running := 0
			for _, p := range s.in.Pods {
				if p.Phase == v1alpha1.NodeRunning {
					running++
				}
			}
			if limit > 0 && running > limit {
				t.Fatalf("%d Pods run with a parallelism of %d:\n%s", running, limit, spec)
			}
			st = r.Status
			if isFinished(st.Phase) {
				break
			}
		}
		if !isFinished(st.Phase) {
			t.Fatalf("the workflow did not finish:\n%s", spec)
		}
		allOK := true
		for name, p := range phases(st) {
			if !IsTerminal(p) {
				t.Fatalf("node %s is %s after the end:\n%s", name, p, spec)
			}
		}
		for _, p := range outcomes {
			allOK = allOK && p == succeeded
		}
		if allOK && st.Phase != v1alpha1.WorkflowSucceeded && !strings.Contains(spec, ".Failed") &&
			!strings.Contains(spec, ".Errored") && !strings.Contains(spec, ".Omitted") && !strings.Contains(spec, ".Skipped") {
			t.Fatalf("all Pods succeeded but the workflow is %s:\n%s", st.Phase, spec)
		}
	})
}

// checkStartOrder checks that a Pod starts only after its dependencies have finished.
func checkStartOrder(t *testing.T, r Result, deps map[string][]string) {
	t.Helper()
	ph := phases(r.Status)
	for _, a := range r.Actions {
		name := strings.TrimPrefix(a.(CreatePod).NodeName, "wf.")
		for _, d := range deps[name] {
			if !IsTerminal(ph["wf."+d]) {
				t.Fatalf("task %s started before its dependency %s finished", name, d)
			}
		}
	}
}
