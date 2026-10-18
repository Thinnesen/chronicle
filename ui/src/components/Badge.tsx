import "./Badge.css";

// ManagedBadge marks an object that a GitOps tool manages. Such an object changes only through Git.
export function ManagedBadge({ tool }: { tool: string }) {
  if (!tool) return null;
  return (
    <span className="badge" title={`${tool} manages this object. Change it in Git.`}>
      Managed by {tool}
    </span>
  );
}

export function ClusterBadge() {
  return (
    <span className="badge" title="A cluster template is the same for all namespaces.">
      Cluster
    </span>
  );
}
