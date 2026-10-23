import { createContext, useContext } from "react";

// TryContext holds the earlier try of a retried run that the run page shows, or undefined for the latest try.
export const TryContext = createContext<number | undefined>(undefined);

// useTry returns the earlier try that the page shows, or undefined for the latest try.
export function useTry(): number | undefined {
  return useContext(TryContext);
}

// RunUIDContext holds the UID of the run that the page shows. Archived runs can have the same name, so the calls for
// the logs and the artifacts of the run name it by its UID too.
export const RunUIDContext = createContext<string | undefined>(undefined);

// useRunUID returns the UID of the run that the page shows.
export function useRunUID(): string | undefined {
  return useContext(RunUIDContext);
}
