import { stringify, type Pair } from "yaml";

import "./YamlView.css";

// Server-set metadata that a user never writes. The view leaves it out.
const serverFields = ["uid", "resourceVersion", "generation", "creationTimestamp", "managedFields", "selfLink"];
const lastApplied = "kubectl.kubernetes.io/last-applied-configuration";

// withoutServerFields returns a copy of a Kubernetes object without the status and the metadata that the server sets.
export function withoutServerFields(value: unknown): unknown {
  if (typeof value !== "object" || value === null || !("metadata" in value)) return value;
  const obj = value as { metadata?: Record<string, unknown> };
  const metadata = Object.fromEntries(Object.entries(obj.metadata ?? {}).filter(([k]) => !serverFields.includes(k)));
  const annotations = Object.entries((metadata.annotations as Record<string, string> | undefined) ?? {}).filter(([k]) => k !== lastApplied);
  if (annotations.length > 0) metadata.annotations = Object.fromEntries(annotations);
  else delete metadata.annotations;
  const copy: Record<string, unknown> = { ...obj, metadata };
  delete copy.status;
  return copy;
}

// The server sends the keys in alphabetical order. These keys come first, as in the YAML that users write.
const firstKeys = ["apiVersion", "kind", "metadata", "name"];
const rank = (p: Pair) => {
  const i = firstKeys.indexOf(String(p.key));
  return i < 0 ? firstKeys.length : i;
};

// toYaml writes an object as YAML with the usual Kubernetes key order.
export function toYaml(value: unknown): string {
  return stringify(value, { lineWidth: 0, sortMapEntries: (a, b) => rank(a) - rank(b) || String(a.key).localeCompare(String(b.key)) });
}

// YamlView shows an object as read-only YAML, the format that users write.
export function YamlView({ value, label }: { value: unknown; label: string }) {
  return (
    <pre className="yaml" tabIndex={0} aria-label={label}>
      {toYaml(withoutServerFields(value))}
    </pre>
  );
}
