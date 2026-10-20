package cli

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/spf13/cobra"
	yamlv3 "go.yaml.in/yaml/v3"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	chroniclev1 "github.com/Thinnesen/chronicle/gen/chronicle/v1"
)

func (a *app) submitCommand() *cobra.Command {
	var file, from, at string
	var params, labels []string
	var watch, log bool
	cmd := &cobra.Command{
		Use:   "submit (-f <file> | --from <template>)",
		Short: "Start a run from a Workflow file or a template",
		Example: "  chronicle submit -f hello.yaml -p who=world --watch\n" +
			"  chronicle submit --from nightly-build\n  chronicle submit --from cluster/base-pipeline\n" +
			"  chronicle submit --from backfill --at 2026-10-21T03:00:00+02:00",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			req, err := a.submitRequest(file, from, params, labels)
			if err != nil {
				return err
			}
			if at != "" {
				t, err := startTime(at, time.Now())
				if err != nil {
					return err
				}
				if time.Until(t) <= 0 {
					_, _ = fmt.Fprintln(a.errOut, "The start time has passed, so the run starts now.")
				}
				req.StartAt = timestamppb.New(t)
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
			if req.StartAt != nil {
				_, _ = fmt.Fprintf(a.out, "Submitted %s/%s. It starts at %s.\n", req.Namespace, name, req.StartAt.AsTime().Local().Format(time.RFC3339))
			} else {
				_, _ = fmt.Fprintf(a.out, "Started %s/%s\n", req.Namespace, name)
			}
			switch {
			case watch:
				return a.watch(cmd.Context(), req.Namespace, name)
			case log:
				return a.followRun(cmd.Context(), req.Namespace, name)
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&file, "file", "f", "", `A Workflow file in YAML or JSON, or "-" for the standard input.`)
	cmd.Flags().StringVar(&from, "from", "", `A WorkflowTemplate name, or "cluster/<name>" for a ClusterWorkflowTemplate.`)
	cmd.Flags().StringArrayVarP(&params, "parameter", "p", nil, "A workflow parameter as name=value. Repeat it for more parameters.")
	cmd.Flags().StringArrayVarP(&labels, "label", "l", nil, "A label of the run as key=value.")
	cmd.Flags().BoolVarP(&watch, "watch", "w", false, "Watch the run until it ends.")
	cmd.Flags().BoolVar(&log, "log", false, "Print the logs of all steps until the run ends.")
	cmd.Flags().StringVar(&at, "at", "", "Start the run later: an RFC 3339 time, or a duration from now such as 2h.")
	cmd.MarkFlagsMutuallyExclusive("watch", "log")
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
	// YAML 1.2 (yaml/v3) reads only true and false as booleans. YAML 1.1 (kubectl) also reads n, yes and off.
	var doc yamlv3.Node
	if err := yamlv3.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", file, err)
	}
	v, err := plain(&doc)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", file, err)
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("parse %s: the file is not an object", file)
	}
	return structpb.NewStruct(m)
}

// merge adds the keys of a "<<" merge value that m does not have yet. The merge value is a map or a list of maps.
func merge(m map[string]any, v any) {
	maps, ok := v.([]any)
	if !ok {
		maps = []any{v}
	}
	for _, x := range maps {
		src, _ := x.(map[string]any)
		for k, val := range src {
			if _, set := m[k]; !set {
				m[k] = val
			}
		}
	}
}

// plain converts a YAML node to maps, lists and scalars. A date stays a string, as Kubernetes expects.
func plain(n *yamlv3.Node) (any, error) {
	switch n.Kind {
	case yamlv3.DocumentNode:
		if len(n.Content) == 0 {
			return nil, nil
		}
		return plain(n.Content[0])
	case yamlv3.AliasNode:
		return plain(n.Alias)
	case yamlv3.MappingNode:
		m := make(map[string]any, len(n.Content)/2)
		for i := 0; i+1 < len(n.Content); i += 2 {
			v, err := plain(n.Content[i+1])
			if err != nil {
				return nil, err
			}
			if n.Content[i].Tag == "!!merge" {
				merge(m, v)
				continue
			}
			m[n.Content[i].Value] = v
		}
		return m, nil
	case yamlv3.SequenceNode:
		l := make([]any, 0, len(n.Content))
		for _, c := range n.Content {
			v, err := plain(c)
			if err != nil {
				return nil, err
			}
			l = append(l, v)
		}
		return l, nil
	}
	if n.Tag == "!!timestamp" {
		return n.Value, nil
	}
	var v any
	return v, n.Decode(&v)
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
	var all bool
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List the runs in a namespace, newest first",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			phases, err := knownPhases(phases)
			if err != nil {
				return err
			}
			req := &chroniclev1.ListWorkflowsRequest{LabelSelector: selector, Phases: phases, AllNamespaces: all}
			if !all {
				ns, err := a.ns()
				if err != nil {
					return err
				}
				req.Namespace = ns
			}
			c, err := a.workflows(cmd.Context())
			if err != nil {
				return err
			}
			resp, err := c.ListWorkflows(cmd.Context(), connect.NewRequest(req))
			if err != nil {
				return err
			}
			printList(a.out, resp.Msg.Workflows, all)
			if resp.Msg.ArchiveUnavailable {
				_, _ = fmt.Fprintln(a.errOut, "The archive did not answer: the list shows only the runs in the cluster.")
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&selector, "selector", "l", "", "A label selector, for example team=a.")
	cmd.Flags().StringSliceVar(&phases, "phase", nil, "Show only runs in these phases, for example Running,Failed.")
	cmd.Flags().BoolVarP(&all, "all-namespaces", "A", false, "List the runs of all namespaces that you can see.")
	return cmd
}

func (a *app) getCommand() *cobra.Command {
	var output string
	var try int32
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
			req := &chroniclev1.GetWorkflowRequest{Namespace: ns, Name: args[0]}
			if try > 0 {
				req.Try = new(try - 1) // The API counts the tries from 0.
			}
			resp, err := c.GetWorkflow(cmd.Context(), connect.NewRequest(req))
			if err != nil {
				return err
			}
			return printWorkflow(a.out, resp.Msg, output)
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "", "The output format: yaml or json. The default is a tree of the nodes.")
	cmd.Flags().Int32Var(&try, "try", 0, "Show an earlier try of a retried run. The first try is 1.")
	return cmd
}

// startTime reads the start time of a run: an RFC 3339 time, or a duration from now.
func startTime(s string, now time.Time) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	if d, err := time.ParseDuration(s); err == nil {
		if d <= 0 {
			return time.Time{}, fmt.Errorf("the duration %q must be positive", s)
		}
		return now.Add(d), nil
	}
	return time.Time{}, fmt.Errorf("%q is not an RFC 3339 time (2026-10-21T03:00:00+02:00) or a duration (2h)", s)
}

// runPhases are the phases of a run, for list --phase.
var runPhases = []string{"Pending", "Running", "Suspended", "Succeeded", "Failed", "Error"}

// knownPhases writes the phases as the API does, for example "failed" as "Failed". An unknown phase is an error.
func knownPhases(phases []string) ([]string, error) {
	out := make([]string, 0, len(phases))
	for _, p := range phases {
		i := slices.IndexFunc(runPhases, func(known string) bool { return strings.EqualFold(known, p) })
		if i < 0 {
			return nil, fmt.Errorf("%q is not a phase: use %s", p, strings.Join(runPhases, ", "))
		}
		out = append(out, runPhases[i])
	}
	return out, nil
}
