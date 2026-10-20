package cli

import (
	"bytes"
	"fmt"
	"io"
	"slices"

	yamlv3 "go.yaml.in/yaml/v3"
)

// yaml11Booleans are the plain words that YAML 1.1 (kubectl, Argo CD) reads as booleans and YAML 1.2 reads as text.
var yaml11Booleans = []string{"y", "Y", "yes", "Yes", "YES", "n", "N", "no", "No", "NO", "on", "On", "ON", "off", "Off", "OFF"}

// yamlTraps warns about plain values that kubectl reads as booleans, for example a parameter value no or a step name n.
func yamlTraps(out io.Writer, file string, data []byte) {
	dec := yamlv3.NewDecoder(bytes.NewReader(data))
	for {
		var doc yamlv3.Node
		if err := dec.Decode(&doc); err != nil {
			return // the end of the file, or a parse error that lintData reports
		}
		walkTraps(out, file, &doc)
	}
}

func walkTraps(out io.Writer, file string, n *yamlv3.Node) {
	if n.Kind == yamlv3.ScalarNode && n.Style == 0 && n.Tag == "!!str" && slices.Contains(yaml11Booleans, n.Value) {
		_, _ = fmt.Fprintf(out, "%s:%d: warning: the value %s is a boolean for kubectl (YAML 1.1); write %q\n", file, n.Line, n.Value, n.Value)
	}
	for _, c := range n.Content {
		walkTraps(out, file, c)
	}
}
