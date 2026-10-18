import "./ProgressTrack.css";

// ProgressTrack is a track with one station for each node. Long runs show a filled line instead.
export function ProgressTrack({ done, total, phase }: { done: number; total: number; phase: string }) {
  const label = `${done} of ${total} nodes done`;
  if (total === 0) {
    return <span className="track-empty" aria-label="No nodes yet" />;
  }
  const failed = phase === "Failed" || phase === "Error";
  if (total > 16) {
    return (
      <span className="track-line" role="img" aria-label={label}>
        <span className={failed ? "track-fill track-fill-failed" : "track-fill"} style={{ width: `${(100 * done) / total}%` }} />
      </span>
    );
  }
  return (
    <span className="track-stations" role="img" aria-label={label}>
      {Array.from({ length: total }, (_, i) => (
        <span key={i} className={i < done ? (failed && i === done - 1 ? "station station-failed" : "station station-done") : "station"} />
      ))}
    </span>
  );
}
