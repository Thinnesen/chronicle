package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	"sigs.k8s.io/yaml"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
	chroniclev1 "github.com/Thinnesen/chronicle/gen/chronicle/v1"
)

func printList(out io.Writer, runs []*chroniclev1.WorkflowSummary) {
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "NAME\tPHASE\tPROGRESS\tSTARTED\tDURATION\tMESSAGE")
	for _, r := range runs {
		started, duration := "-", "-"
		if r.StartedAt != nil {
			started = ago(r.StartedAt.AsTime())
			end := time.Now()
			if r.FinishedAt != nil {
				end = r.FinishedAt.AsTime()
			}
			duration = end.Sub(r.StartedAt.AsTime()).Round(time.Second).String()
		}
		_, _ = fmt.Fprintf(w, "%s\t%s\t%d/%d\t%s\t%s\t%s\n", r.Name, cmpOr(r.Phase, "Pending"), r.NodesDone, r.NodesTotal, started, duration, r.Message)
	}
	_ = w.Flush()
}

func ago(t time.Time) string {
	return time.Since(t).Round(time.Second).String() + " ago"
}

// nodes returns the nodes of a run, sorted by name, from the Workflow object of a GetWorkflow answer.
func nodes(resp *chroniclev1.GetWorkflowResponse) []v1alpha1.NodeStatus {
	wf, err := toWorkflow(resp)
	if err != nil {
		return nil
	}
	out := make([]v1alpha1.NodeStatus, 0, len(wf.Status.Nodes))
	for _, n := range wf.Status.Nodes {
		out = append(out, n)
	}
	slices.SortFunc(out, func(a, b v1alpha1.NodeStatus) int { return strings.Compare(a.Name, b.Name) })
	return out
}

func toWorkflow(resp *chroniclev1.GetWorkflowResponse) (*v1alpha1.Workflow, error) {
	data, err := resp.Workflow.MarshalJSON()
	if err != nil {
		return nil, err
	}
	var wf v1alpha1.Workflow
	return &wf, json.Unmarshal(data, &wf)
}

// printWorkflow prints a run as YAML, as JSON or as a tree of its nodes.
func printWorkflow(out io.Writer, resp *chroniclev1.GetWorkflowResponse, format string) error {
	wf, err := toWorkflow(resp)
	if err != nil {
		return err
	}
	switch format {
	case "json":
		data, _ := json.MarshalIndent(wf, "", "  ")
		_, err = fmt.Fprintln(out, string(data))
		return err
	case "yaml":
		data, _ := yaml.Marshal(wf)
		_, err = out.Write(data)
		return err
	case "":
	default:
		return fmt.Errorf("unknown output format %q: use yaml or json", format)
	}
	s := resp.Summary
	_, _ = fmt.Fprintf(out, "Name:      %s/%s\nPhase:     %s\n", s.Namespace, s.Name, cmpOr(s.Phase, "Pending"))
	if s.Message != "" {
		_, _ = fmt.Fprintf(out, "Message:   %s\n", s.Message)
	}
	_, _ = fmt.Fprintf(out, "Progress:  %d/%d\n\n", s.NodesDone, s.NodesTotal)
	if root, ok := wf.Status.Nodes[wf.Name]; ok {
		printNode(out, wf.Status.Nodes, root, "")
	}
	return nil
}

func printNode(out io.Writer, all map[string]v1alpha1.NodeStatus, n v1alpha1.NodeStatus, indent string) {
	_, _ = fmt.Fprintf(out, "%s%s %s%s\n", indent, symbol(n.Phase), cmpOr(n.DisplayName, n.Name), suffix(n.Message))
	for _, id := range n.Children {
		printNode(out, all, all[id], indent+"  ")
	}
}

func symbol(p v1alpha1.NodePhase) string {
	switch p {
	case v1alpha1.NodeSucceeded:
		return "✔"
	case v1alpha1.NodeFailed, v1alpha1.NodeError:
		return "✖"
	case v1alpha1.NodeRunning:
		return "●"
	case v1alpha1.NodeSkipped, v1alpha1.NodeOmitted:
		return "○"
	}
	return "◌"
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
