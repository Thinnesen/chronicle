import { createContext, useContext } from "react";

// TryContext holds the earlier try of a retried run that the run page shows, or undefined for the latest try.
export const TryContext = createContext<number | undefined>(undefined);

// useTry returns the earlier try that the page shows, or undefined for the latest try.
export function useTry(): number | undefined {
  return useContext(TryContext);
}
