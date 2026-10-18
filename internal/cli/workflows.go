package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"connectrpc.com/connect"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/types/known/structpb"
	"sigs.k8s.io/yaml"

	chroniclev1 "github.com/Thinnesen/chronicle/gen/chronicle/v1"
)

func (a *app) submitCommand() *cobra.Command {
	var file, from string
	var params, labels []string
	var watch bool
	cmd := &cobra.Command{
		Use:   "submit (-f <file> | --from <template>)",
		Short: "Start a run from a Workflow file or a template",
		Example: "  chronicle submit -f hello.yaml -p who=world --watch\n" +
			"  chronicle submit --from nightly-build\n  chronicle submit --from cluster/base-pipeline",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			req, err := a.submitRequest(file, from, params, labels)
			if err != nil {
				return err
			}
			c, err := a.workflows(cmd.Context())
			if err != nil {
				return err
			}
			resp, err := c.SubmitWorkflow(cmd.Context(), connect.NewRequest(req))
			if err != nil {
				return err
			}
			name := resp.Msg.Summary.Name
			_, _ = fmt.Fprintf(a.out, "Started %s/%s\n", req.Namespace, name)
			if watch {
				return a.watch(cmd.Context(), req.Namespace, name)
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&file, "file", "f", "", `A Workflow file in YAML or JSON, or "-" for the standard input.`)
	cmd.Flags().StringVar(&from, "from", "", `A WorkflowTemplate name, or "cluster/<name>" for a ClusterWorkflowTemplate.`)
	cmd.Flags().StringArrayVarP(&params, "parameter", "p", nil, "A workflow parameter as name=value. Repeat it for more parameters.")
	cmd.Flags().StringArrayVarP(&labels, "label", "l", nil, "A label of the run as key=value.")
	cmd.Flags().BoolVarP(&watch, "watch", "w", false, "Watch the run until it ends.")
	return cmd
}

func (a *app) submitRequest(file, from string, params, labels []string) (*chroniclev1.SubmitWorkflowRequest, error) {
	ns, err := a.ns()
	if err != nil {
		return nil, err
	}
	p, err := keyValues(params)
	if err != nil {
		return nil, err
	}
	l, err := keyValues(labels)
	if err != nil {
		return nil, err
	}
	req := &chroniclev1.SubmitWorkflowRequest{Namespace: ns, Parameters: p, Labels: l}
	switch {
	case (file == "") == (from == ""):
		return nil, errors.New("give -f <file> or --from <template>")
	case from != "":
		name, cluster := strings.CutPrefix(from, "cluster/")
		req.Source = &chroniclev1.SubmitWorkflowRequest_Template{Template: &chroniclev1.TemplateSource{Name: name, ClusterScope: cluster}}
	default:
		obj, err := readObject(file)
		if err != nil {
			return nil, err
		}
		req.Source = &chroniclev1.SubmitWorkflowRequest_Workflow{Workflow: obj}
	}
	return req, nil
}

func readObject(file string) (*structpb.Struct, error) {
	var data []byte
	var err error
	if file == "-" {
		data, err = readAllLimited(os.Stdin)
	} else {
		data, err = os.ReadFile(file)
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", file, err)
	}
	var m map[string]any
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse %s: %w", file, err)
	}
	return structpb.NewStruct(m)
}

func keyValues(pairs []string) (map[string]string, error) {
	out := map[string]string{}
	for _, kv := range pairs {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || k == "" {
			return nil, fmt.Errorf("%q is not name=value", kv)
		}
		out[k] = v
	}
	return out, nil
}

func (a *app) listCommand() *cobra.Command {
	var selector string
	var phases []string
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List the runs in a namespace, newest first",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ns, err := a.ns()
			if err != nil {
				return err
			}
			c, err := a.workflows(cmd.Context())
			if err != nil {
				return err
			}
			resp, err := c.ListWorkflows(cmd.Context(), connect.NewRequest(&chroniclev1.ListWorkflowsRequest{
				Namespace: ns, LabelSelector: selector, Phases: phases}))
			if err != nil {
				return err
			}
			printList(a.out, resp.Msg.Workflows)
			return nil
		},
	}
	cmd.Flags().StringVarP(&selector, "selector", "l", "", "A label selector, for example team=a.")
	cmd.Flags().StringSliceVar(&phases, "phase", nil, "Show only runs in these phases, for example Running,Failed.")
	return cmd
}

func (a *app) getCommand() *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:   "get <name>",
		Short: "Show a run and its nodes",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ns, err := a.ns()
			if err != nil {
				return err
			}
			c, err := a.workflows(cmd.Context())
			if err != nil {
				return err
			}
			resp, err := c.GetWorkflow(cmd.Context(), connect.NewRequest(&chroniclev1.GetWorkflowRequest{Namespace: ns, Name: args[0]}))
			if err != nil {
				return err
			}
			return printWorkflow(a.out, resp.Msg, output)
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "", "The output format: yaml or json. The default is a tree of the nodes.")
	return cmd
}
