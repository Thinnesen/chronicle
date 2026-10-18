import "./Lamp.css";

// Lamp shows a phase as a signal lamp. The shape differs for each phase, so color is never the only signal.
export function Lamp({ phase, size = 12 }: { phase: string; size?: number }) {
  const kind = lampKind(phase);
  return (
    <svg className={`lamp lamp-${kind}`} width={size} height={size} viewBox="0 0 12 12" aria-hidden="true">
      {kind === "proceed" && <circle cx="6" cy="6" r="5" />}
      {kind === "running" && <circle cx="6" cy="6" r="4" className="lamp-ring" />}
      {kind === "danger" && <path d="M2 2l8 8M10 2l-8 8" className="lamp-cross" />}
      {kind === "caution" && <path d="M6 0.5L11.5 6L6 11.5L0.5 6z" />}
      {kind === "idle" && <circle cx="6" cy="6" r="4" className="lamp-ring" />}
    </svg>
  );
}

export type LampKind = "proceed" | "running" | "danger" | "caution" | "idle";

export function lampKind(phase: string): LampKind {
  switch (phase) {
    case "Succeeded":
      return "proceed";
    case "Running":
      return "running";
    case "Failed":
    case "Error":
      return "danger";
    case "Suspended":
      return "caution";
    default:
      return "idle";
  }
}
