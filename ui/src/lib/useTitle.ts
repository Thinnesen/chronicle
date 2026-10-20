import { useEffect } from "react";

// useTitle sets the title of the browser tab, so that a user with many tabs finds the run or page.
export function useTitle(title: string | undefined) {
  useEffect(() => {
    document.title = title ? `${title} - chronicle` : "chronicle";
  }, [title]);
}
