import "./Lamp.css";

// Lamp shows a phase as a status icon. The shape differs for each phase, so color is never the only signal.
export function Lamp({ phase, size = 14 }: { phase: string; size?: number }) {
  const kind = lampKind(phase);
  return (
    <svg className={`lamp lamp-${kind}`} width={size} height={size} viewBox="0 0 16 16" aria-hidden="true">
      {kind === "proceed" && (
        <>
          <circle cx="8" cy="8" r="8" className="lamp-fill" />
          <path d="M4.6 8.3l2.2 2.2 4.6-4.8" className="lamp-mark" />
        </>
      )}
      {kind === "danger" && (
        <>
          <circle cx="8" cy="8" r="8" className="lamp-fill" />
          <path d="M5.4 5.4l5.2 5.2M10.6 5.4l-5.2 5.2" className="lamp-mark" />
        </>
      )}
      {kind === "caution" && (
        <>
          <circle cx="8" cy="8" r="8" className="lamp-fill" />
          <path d="M6.3 5v6M9.7 5v6" className="lamp-mark" />
        </>
      )}
      {kind === "running" && (
        <>
          <circle cx="8" cy="8" r="6.5" className="lamp-track" />
          <path d="M8 1.5a6.5 6.5 0 0 1 6.5 6.5" className="lamp-arc" />
          <circle cx="8" cy="8" r="2.5" className="lamp-fill" />
        </>
      )}
      {kind === "idle" && <circle cx="8" cy="8" r="6.5" className="lamp-dash" />}
    </svg>
  );
}

// Status is a phase as a pill with an icon and the phase name.
export function Status({ phase }: { phase: string }) {
  return (
    <span className={`status status-${lampKind(phase)}`}>
      <Lamp phase={phase} size={12} />
      {phase}
    </span>
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
