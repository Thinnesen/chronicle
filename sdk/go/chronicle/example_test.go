package chronicle_test

import (
	"fmt"
	"time"

	"github.com/Thinnesen/chronicle/sdk/go/chronicle"
)

// This example builds a DAG: fetch saves a file, two tasks use it in parallel, and report runs last.
func Example() {
	w := chronicle.New("etl-").Label("team", "data")
	day := w.Param("day", "2026-10-17")

	const source = `mkdir -p /tmp/rows && echo "$DAY" > /tmp/rows/day.txt && echo 1204`
	fetch := chronicle.Script("fetch", "busybox:1.37", source).
		Env("DAY", day).
		OutputArtifact("rows", "/tmp/rows").
		Retry(2, 10*time.Second)
	count := chronicle.Container("count", "busybox:1.37", "wc", "-l", "/tmp/in/day.txt").InputArtifact("rows", "/tmp/in")
	echo := chronicle.Container("echo", "busybox:1.37", "echo")
	echo.Args(echo.Input("msg"))

	main := chronicle.DAG("main")
	f := main.Task("fetch", fetch)
	c := main.Task("count", count).After(f).ArtifactArg("rows", f.Artifact("rows"))
	main.Task("report", echo).After(c).Arg("msg", "fetched "+f.Result()+" rows")
	w.Entrypoint(main)

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
	//   templates:
	//   - dag:
	//       tasks:
	//       - name: fetch
	//         template: fetch
	//       - arguments:
	//           artifacts:
	//           - from: '{{tasks.fetch.outputs.artifacts.rows}}'
	//             name: rows
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
