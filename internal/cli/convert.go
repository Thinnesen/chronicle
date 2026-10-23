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
			"resources. A file can have more than one document. The command needs no server.\n\n" +
			"The result goes to standard output. The warnings go to standard error. Each warning names a field and what " +
			"happened to it. A field that chronicle does not support is dropped, so read each warning: the behavior can " +
			"change.\n\n" +
			"Main changes: a retry without a limit gets limit 10. Placeholders in a shell script source become environment " +
			"variables. A resource action replace becomes apply. A step that creates a Workflow from a template becomes a " +
			"childWorkflow template.\n\n" +
			"The command validates the result with the rules of the admission webhook. Exit code 1 means that the result " +
			"is not valid, for example because of an expression ({{=...}}), a placeholder that chronicle does not have, or " +
			"placeholders in a script that is not a shell script. With --strict, a warning also gives exit code 1.",
		Example: "  chronicle convert -f argo-workflow.yaml > workflow.yaml\n" +
			"  chronicle convert --strict -f argo.yaml | chronicle lint -\n" +
			"  chronicle convert -f argo.yaml | kubectl create -n team-a -f -",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			data, err := readInput(file, cmd.InOrStdin())
			if err != nil {
				return err
			}
			results, err := convert.Convert(data)
			if err != nil {
				return err
			}
			warned, written := false, 0
			for _, r := range results {
				for _, w := range r.Warnings {
					warned = true
					_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s %s: %s\n", r.Kind, r.Name, w)
				}
				if r.YAML == nil {
					continue // a dropped document
				}
				if written > 0 {
					_, _ = fmt.Fprintln(a.out, "---")
				}
				written++
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
