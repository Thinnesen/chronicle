package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
	"github.com/Thinnesen/chronicle/internal/validate"
)

func (a *app) lintCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "lint <file or directory>...",
		Short: "Check chronicle resources in YAML files without a server",
		Long: "Check the Workflows, WorkflowTemplates, ClusterWorkflowTemplates and CronWorkflows in YAML files with the rules " +
			"of the admission webhook. A directory includes its .yaml, .yml and .json files. Other resources are skipped. " +
			"Use - to read standard input. The command does not check references to other templates. " +
			"The exit code is 1 when a resource has a problem.",
		Example: "  chronicle lint workflows/\n  chronicle lint etl.yaml deploy.yaml",
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			files, err := lintFiles(args)
			if err != nil {
				return err
			}
			total, bad := 0, 0
			for _, f := range files {
				data, err := readInput(f, cmd.InOrStdin())
				if err != nil {
					return err
				}
				n, problems := lintData(a.out, f, data)
				total, bad = total+n, bad+problems
			}
			return lintSummary(a.out, total, bad)
		},
	}
}

// lintFiles expands the directories in args to their YAML and JSON files.
func lintFiles(args []string) ([]string, error) {
	var files []string
	for _, arg := range args {
		if arg == "-" {
			files = append(files, arg)
			continue
		}
		err := filepath.WalkDir(arg, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			// A file in the arguments counts with any name; a file in a directory needs a YAML or JSON name.
			ext := strings.ToLower(filepath.Ext(path))
			if path == arg || ext == ".yaml" || ext == ".yml" || ext == ".json" {
				files = append(files, path)
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", arg, err)
		}
	}
	return files, nil
}

// lintData checks each chronicle resource in one file. It returns the number of resources and of problems.
// A file that is not YAML counts as one problem.
func lintData(out io.Writer, file string, data []byte) (int, int) {
	dec := utilyaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096)
	total, bad := 0, 0
	for {
		var doc map[string]any
		err := dec.Decode(&doc)
		if errors.Is(err, io.EOF) {
			return total, bad
		}
		if err != nil {
			// The rest of the file cannot be read either. The other files still get their check.
			_, _ = fmt.Fprintf(out, "%s: not valid YAML: %v\n", file, err)
			return total + 1, bad + 1
		}
		version, _ := doc["apiVersion"].(string)
		kind, _ := doc["kind"].(string)
		var problem error
		switch {
		case strings.HasPrefix(version, "argoproj.io/"):
			problem = errors.New("an Argo Workflows resource: convert it first with chronicle convert")
		case strings.HasPrefix(version, v1alpha1.GroupVersion.Group+"/"):
			raw, _ := json.Marshal(doc) // A decoded YAML document always encodes.
			problem = validate.Resource(raw)
		default:
			continue
		}
		total++
		if problem != nil {
			bad++
			_, _ = fmt.Fprintf(out, "%s: %s %s: %v\n", file, kind, docName(doc), problem)
		} else {
			_, _ = fmt.Fprintf(out, "%s: %s %s is valid\n", file, kind, docName(doc))
		}
	}
}

func docName(doc map[string]any) string {
	meta, _ := doc["metadata"].(map[string]any)
	if n, ok := meta["name"].(string); ok {
		return n
	}
	n, _ := meta["generateName"].(string)
	return n
}

func lintSummary(out io.Writer, total, bad int) error {
	switch {
	case total == 0:
		return errors.New("the files have no chronicle resources")
	case bad > 0:
		_, _ = fmt.Fprintf(out, "%d of %d resources have problems\n", bad, total)
		return errors.New("lint found problems")
	case total == 1:
		_, _ = fmt.Fprintln(out, "1 resource is valid")
	default:
		_, _ = fmt.Fprintf(out, "%d resources are valid\n", total)
	}
	return nil
}
