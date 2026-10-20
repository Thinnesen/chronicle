package cli

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/Thinnesen/chronicle/internal/convert"
)

func (a *app) convertCommand() *cobra.Command {
	var file string
	var strict bool
	cmd := &cobra.Command{
		Use:   "convert -f <file>",
		Short: "Convert Argo Workflows YAML into chronicle resources",
		Long: "Convert Argo Workflows YAML (Workflow, WorkflowTemplate, ClusterWorkflowTemplate, CronWorkflow) into chronicle " +
			"resources. The result goes to standard output. The warnings go to standard error. Each warning names a field " +
			"that chronicle does not support. The result does not have that field. Use - to read standard input.",
		Example: "  chronicle convert -f argo-workflow.yaml > workflow.yaml\n  chronicle convert -f argo.yaml | kubectl create -f -",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			data, err := readInput(file, cmd.InOrStdin())
			if err != nil {
				return err
			}
			results, err := convert.Convert(data)
			if err != nil {
				return err
			}
			warned := false
			for i, r := range results {
				for _, w := range r.Warnings {
					warned = true
					_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s %s: %s\n", r.Kind, r.Name, w)
				}
				if i > 0 {
					_, _ = fmt.Fprintln(a.out, "---")
				}
				_, _ = a.out.Write(r.YAML)
			}
			if strict && warned {
				return errors.New("the conversion has warnings")
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&file, "file", "f", "", "The Argo YAML file, or - for standard input.")
	cmd.Flags().BoolVar(&strict, "strict", false, "Exit with an error when a field was dropped.")
	_ = cmd.MarkFlagRequired("file")
	return cmd
}

func readInput(file string, stdin io.Reader) ([]byte, error) {
	if file == "-" {
		return readAllLimited(stdin)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", file, err)
	}
	return data, nil
}
