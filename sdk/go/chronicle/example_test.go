package chronicle_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"sigs.k8s.io/yaml"

	"github.com/Thinnesen/chronicle/sdk/go/chronicle"
)

// etl builds a DAG: fetch saves a directory, count uses it, and report runs last.
// The Python and Rust SDK tests build the same workflow and compare it with sdk/testdata/etl.json.
func etl() *chronicle.Workflow {
	w := chronicle.New("etl-").Label("team", "data").Parallelism(4).Mutex("etl")
	day := w.Param("day", "2026-10-17")

	const source = `mkdir -p /tmp/rows && echo "$DAY" > /tmp/rows/day.txt && echo 1204`
	fetch := chronicle.Script("fetch", "busybox:1.37", source).
		Env("DAY", day).
		OutputArtifact("rows", "/tmp/rows").
		Retry(2, 10*time.Second)
	count := chronicle.Container("count", "busybox:1.37", "wc", "-l", "/tmp/in/day.txt").InputArtifact("rows", "/tmp/in")
	echo := chronicle.Container("echo", "busybox:1.37", "echo")
	echo.Args(echo.Input("msg"))

	main := chronicle.DAG("main").Parallelism(2)
	f := main.Task("fetch", fetch)
	c := main.Task("count", count).After(f).ArtifactArg("rows", f.Artifact("rows")).ContinueOn(true, false)
	main.Task("report", echo).After(c).Arg("msg", "fetched "+f.Result()+" rows")
	return w.Entrypoint(main)
}

// TestSharedFixture keeps sdk/testdata/etl.json current. Run it with UPDATE=1 to write the file.
func TestSharedFixture(t *testing.T) {
	out, err := etl().YAML()
	if err != nil {
		t.Fatal(err)
	}
	got, err := yaml.YAMLToJSON(out)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, got, "", "  "); err != nil {
		t.Fatal(err)
	}
	buf.WriteByte('\n')
	const path = "../../testdata/etl.json"
	if os.Getenv("UPDATE") != "" {
		if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(want, buf.Bytes()) {
		t.Errorf("%s is not current (%v): run the test with UPDATE=1", path, err)
	}
}

// This example renders the workflow of etl as YAML.
func Example() {
	w := etl()
	out, err := w.YAML()
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Print(string(out))
	// Output:
	// apiVersion: chronicle.thinnesen.com/v1alpha1
	// kind: Workflow
	// metadata:
	//   generateName: etl-
	//   labels:
	//     team: data
	// spec:
	//   arguments:
	//     parameters:
	//     - name: day
	//       value: "2026-10-17"
	//   entrypoint: main
	//   parallelism: 4
	//   synchronization:
	//     mutex:
	//       name: etl
	//   templates:
	//   - dag:
	//       tasks:
	//       - name: fetch
	//         template: fetch
	//       - arguments:
	//           artifacts:
	//           - from: '{{tasks.fetch.outputs.artifacts.rows}}'
	//             name: rows
	//         continueOn:
	//           failed: true
	//         dependencies:
	//         - fetch
	//         name: count
	//         template: count
	//       - arguments:
	//           parameters:
	//           - name: msg
	//             value: fetched {{tasks.fetch.outputs.result}} rows
	//         dependencies:
	//         - count
	//         name: report
	//         template: echo
	//     name: main
	//     parallelism: 2
	//   - name: fetch
	//     outputs:
	//       artifacts:
	//       - name: rows
	//         path: /tmp/rows
	//     retryStrategy:
	//       backoff:
	//         duration: 10s
	//         factor: 2
	//       limit: 2
	//     script:
	//       env:
	//       - name: DAY
	//         value: '{{workflow.parameters.day}}'
	//       image: busybox:1.37
	//       source: mkdir -p /tmp/rows && echo "$DAY" > /tmp/rows/day.txt && echo 1204
	//   - container:
	//       command:
	//       - wc
	//       - -l
	//       - /tmp/in/day.txt
	//       image: busybox:1.37
	//     inputs:
	//       artifacts:
	//       - name: rows
	//         path: /tmp/in
	//     name: count
	//   - container:
	//       args:
	//       - '{{inputs.parameters.msg}}'
	//       command:
	//       - echo
	//       image: busybox:1.37
	//     inputs:
	//       parameters:
	//       - name: msg
	//     name: echo
}
