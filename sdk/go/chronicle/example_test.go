package chronicle_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/yaml"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
	"github.com/Thinnesen/chronicle/sdk/go/chronicle"
)

// etl builds a DAG: fetch saves a directory, count uses it, and report runs last.
// The Python and Rust SDK tests build the same workflow and compare it with sdk/testdata/etl.json.
func etl() *chronicle.Workflow {
	w := chronicle.New("etl-").Label("team", "data").Parallelism(4).SemaphoreFrom("etl", "limits", "etl").
		VolumeClaim("cache", "1Gi").Volume(v1alpha1.Volume{Name: "scratch", EmptyDir: &corev1.EmptyDirVolumeSource{}})
	day := w.Param("day", "2026-10-17")

	const source = `mkdir -p /tmp/rows && echo "$DAY" > /tmp/rows/day.txt && echo 1204`
	fetch := chronicle.Script("fetch", "busybox:1.37", source).
		Env("DAY", day).
		OutputArtifact("rows", "/tmp/rows").
		Retry(2, 10*time.Second).
		Mount("cache", "/cache").Mount("scratch", "/scratch")
	count := chronicle.Container("count", "busybox:1.37", "wc", "-l", "/tmp/in/day.txt").
		InputArtifact("rows", "/tmp/in").Mutex("counter")
	echo := chronicle.Container("echo", "busybox:1.37", "echo")
	echo.Args(echo.Input("msg"))

	main := chronicle.DAG("main").Parallelism(2)
	f := main.Task("fetch", fetch)
	c := main.Task("count", count).After(f).ArtifactArg("rows", f.Artifact("rows")).ContinueOn(true, false)
	main.Task("report", echo).After(c).Arg("msg", "fetched "+f.Result()+" rows").
		OnExit(echo, map[string]string{"msg": "count was " + c.Status()})
	main.Task("parts", echo).After(c).WithSequence("3", 1, "part-%d").Arg("msg", "{{item}}")
	notify := chronicle.HTTP("notify", "https://chat.example.com/hooks/data").Method("POST").
		Header("Content-Type", "application/json").HeaderFromSecret("Authorization", "chat", "token").
		Body(`{"text": "{{workflow.name}} is {{workflow.status}}"}`)
	return w.Entrypoint(main).OnExit(notify)
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
	//   onExit: notify
	//   parallelism: 4
	//   synchronization:
	//     semaphore:
	//       configMapKeyRef:
	//         key: etl
	//         name: limits
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
	//         onExit:
	//           arguments:
	//             parameters:
	//             - name: msg
	//               value: count was {{tasks.count.status}}
	//           template: echo
	//         template: echo
	//       - arguments:
	//           parameters:
	//           - name: msg
	//             value: '{{item}}'
	//         dependencies:
	//         - count
	//         name: parts
	//         template: echo
	//         withSequence:
	//           count: "3"
	//           format: part-%d
	//           start: 1
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
	//       volumeMounts:
	//       - mountPath: /cache
	//         name: cache
	//       - mountPath: /scratch
	//         name: scratch
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
	//     synchronization:
	//       mutex:
	//         name: counter
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
	//   - http:
	//       body: '{"text": "{{workflow.name}} is {{workflow.status}}"}'
	//       headers:
	//       - name: Content-Type
	//         value: application/json
	//       - name: Authorization
	//         valueFrom:
	//           secretKeyRef:
	//             key: token
	//             name: chat
	//       method: POST
	//       url: https://chat.example.com/hooks/data
	//     name: notify
	//   volumeClaimTemplates:
	//   - name: cache
	//     storage: 1Gi
	//   volumes:
	//   - emptyDir: {}
	//     name: scratch
}
