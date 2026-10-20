package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"

	"github.com/Thinnesen/chronicle/api/v1alpha1"
	chroniclev1 "github.com/Thinnesen/chronicle/gen/chronicle/v1"
)

func printList(out io.Writer, runs []*chroniclev1.WorkflowSummary, namespaces bool) {
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	if namespaces {
		_, _ = fmt.Fprint(w, "NAMESPACE\t")
	}
	_, _ = fmt.Fprintln(w, "NAME\tPHASE\tPROGRESS\tSTARTED\tDURATION\tMESSAGE")
	for _, r := range runs {
		if namespaces {
			_, _ = fmt.Fprint(w, r.Namespace+"\t")
		}
		started, duration := "-", "-"
		if r.StartedAt != nil {
			started = ago(r.StartedAt.AsTime())
			end := time.Now()
			if r.FinishedAt != nil {
				end = r.FinishedAt.AsTime()
			}
			duration = end.Sub(r.StartedAt.AsTime()).Round(time.Second).String()
		}
		message := r.Message
		switch {
		case message != "" && r.Archived:
			message += " (only in the archive)"
		case message != "":
		case r.ApprovalsWaiting > 0:
			message = "waiting for an approval"
		case r.WaitingReason != "":
			message = "a step waits: " + r.WaitingReason
		case r.Archived:
			message = "only in the archive"
		}
		_, _ = fmt.Fprintf(w, "%s\t%s\t%d/%d\t%s\t%s\t%s\n", r.Name, cmpOr(r.Phase, "Pending"), r.NodesDone, r.NodesTotal, started, duration, message)
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
	_, _ = fmt.Fprintf(out, "Name:      %s/%s\n", s.Namespace, s.Name)
	if parent := wf.Annotations["chronicle.thinnesen.com/parent"]; parent != "" {
		_, _ = fmt.Fprintf(out, "Parent:    %s\n", parent)
	}
	_, _ = fmt.Fprintf(out, "Phase:     %s\n", cmpOr(s.Phase, "Pending"))
	if s.Message != "" {
		_, _ = fmt.Fprintf(out, "Message:   %s\n", s.Message)
	}
	if s.StartedAt != nil {
		started := s.StartedAt.AsTime().Local().Format(time.DateTime)
		_, _ = fmt.Fprintf(out, "Started:   %s\nDuration:  %s\n", started, took(wf.Status.StartedAt, wf.Status.FinishedAt))
	}
	if o := wf.Status.Outputs; o != nil && len(o.Parameters) > 0 {
		parts := make([]string, 0, len(o.Parameters))
		for _, p := range o.Parameters {
			parts = append(parts, p.Name+"="+ptrValue(p.Value))
		}
		_, _ = fmt.Fprintf(out, "Outputs:   %s\n", strings.Join(parts, ", "))
	}
	if o := wf.Status.Outputs; o != nil && len(o.Artifacts) > 0 {
		names := make([]string, 0, len(o.Artifacts))
		for _, a := range o.Artifacts {
			names = append(names, a.Name)
		}
		_, _ = fmt.Fprintf(out, "Artifacts: %s\n", strings.Join(names, ", "))
	}
	if len(resp.Tries) > 0 {
		_, _ = fmt.Fprintf(out, "Tries:     %s\n", triesLine(resp.Tries, s.Archived, wf.Status.Restarts))
	}
	_, _ = fmt.Fprintf(out, "Progress:  %d/%d\n\n", s.NodesDone, s.NodesTotal)
	if root, ok := wf.Status.Nodes[wf.Name]; ok {
		tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		_, _ = fmt.Fprintln(tw, "STEP\tDURATION\tMESSAGE")
		printNode(tw, wf.Status.Nodes, root, "")
		// The exit handler of the workflow is not under the root node.
		for _, n := range wf.Status.Nodes {
			if n.Name == wf.Name+".onExit" {
				printNode(tw, wf.Status.Nodes, n, "")
			}
		}
		if err := tw.Flush(); err != nil {
			return err
		}
	}
	if s.ApprovalsWaiting > 0 {
		node := ""
		if s.ApprovalsWaiting > 1 {
			node = " <node>"
		}
		_, err = fmt.Fprintf(out, "\nApprove with: chronicle approve %s%s --namespace %s\n", s.Name, node, s.Namespace)
	}
	return err
}

func printNode(out io.Writer, all map[string]v1alpha1.NodeStatus, n v1alpha1.NodeStatus, indent string) {
	msg := n.Message
	if msg == "" && n.ChildWorkflow != "" {
		msg = "child run " + n.ChildWorkflow
	}
	_, _ = fmt.Fprintf(out, "%s%s %s\t%s\t%s\n", indent, symbol(n.Phase), cmpOr(n.DisplayName, n.Name), took(n.StartedAt, n.FinishedAt), msg)
	for _, id := range n.Children {
		printNode(out, all, all[id], indent+"  ")
	}
}

// took is the time from start to end, or to now while the node runs. It is empty before the start.
func took(start, end *metav1.Time) string {
	if start == nil {
		return ""
	}
	stop := time.Now()
	if end != nil {
		stop = end.Time
	}
	return stop.Sub(start.Time).Round(time.Second).String()
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

func ptrValue(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// triesLine lists the tries of a retried run, from 1 as in the UI, and marks the try that the output shows. An earlier
// try comes from the archive, so it has the retry count of that try.
func triesLine(tries []*chroniclev1.Try, archived bool, restarts int32) string {
	parts := make([]string, 0, len(tries)+1)
	shownEarlier := false
	for _, t := range tries {
		part := fmt.Sprintf("%d %s", t.Number+1, t.Phase)
		if archived && t.Number == restarts {
			part += " (shown)"
			shownEarlier = true
		}
		parts = append(parts, part)
	}
	if shownEarlier {
		return strings.Join(append(parts, "latest."), ", ")
	}
	return strings.Join(append(parts, "latest (shown). Show a try with --try <number>."), ", ")
}
