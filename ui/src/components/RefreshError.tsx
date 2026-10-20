import { errorText } from "../lib/errors";

// RefreshError says that the page could not load new data and still shows the last data, for example while the
// server restarts. A page without data shows its own error instead.
export function RefreshError({ error }: { error: Error | null }) {
  if (!error) return null;
  return (
    <p className="refresh-error" role="status">
      Could not refresh: {errorText(error)}. The page shows the last data and tries again.
    </p>
  );
}
