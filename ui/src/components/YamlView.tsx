import { stringify } from "yaml";

import "./YamlView.css";

// YamlView shows an object as read-only YAML, the format that users write.
export function YamlView({ value, label }: { value: unknown; label: string }) {
  return (
    <pre className="yaml" tabIndex={0} aria-label={label}>
      {stringify(value, { lineWidth: 0 })}
    </pre>
  );
}
