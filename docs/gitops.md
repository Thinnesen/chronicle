# GitOps

Every chronicle definition is a Kubernetes resource: `WorkflowTemplate`, `ClusterWorkflowTemplate`, `CronWorkflow` and even a single `Workflow`. Keep them in Git and let Argo CD or Flux apply them, like any other resource.

## App of apps with Argo CD

`deploy/argocd/` is a complete example:

| File | What it installs |
|---|---|
| `root.yaml` | The root application. It syncs the applications in `apps/`. |
| `apps/cert-manager.yaml` | cert-manager, for the certificate of the admission webhook (sync wave -1). |
| `apps/chronicle.yaml` | chronicle from the Helm chart `dist/chart` at the release tag, with the login and the teams as Helm values. |
| `apps/team-a-workflows.yaml` | The templates and cron workflows of team A from `workflows/team-a/` (sync wave 1). |

1. Fork the repository. Change `repoURL` in the files to your fork.
2. Set the image, the login (see [entra-id.md](entra-id.md) for Microsoft 365) and the teams in `apps/chronicle.yaml`.
3. Create the Secret with the OIDC client secret in `chronicle-system`.
4. `kubectl apply -n argocd -f deploy/argocd/root.yaml`.

The chronicle application uses `ServerSideApply=true`, because the Workflow CRD is too large for client-side apply.

## How the UI treats resources from Git

- chronicle reads the marks of Argo CD: the annotation `argocd.argoproj.io/tracking-id` or the label `argocd.argoproj.io/instance`. It reads the marks of Flux: the labels `kustomize.toolkit.fluxcd.io/name` and `helm.toolkit.fluxcd.io/name`.
- The UI and the API show such a template or cron workflow as "Managed by Argo CD" or "Managed by Flux". They refuse changes to it, for example a suspend of a cron workflow. Change it in Git.
- Runs that start from a managed template, by hand or from a cron workflow, are normal Workflows outside Git. Argo CD does not prune them, because they have no tracking label.
