import { Code, ConnectError } from "@connectrpc/connect";
import type { ReactNode } from "react";

import { errorText } from "../lib/errors";

// PageError replaces a page that could not load its object. missing is the sentence for an object that does not
// exist; children give the way back.
export function PageError({ error, what, missing, children }: { error: unknown; what: string; missing: string; children: ReactNode }) {
  const notFound = ConnectError.from(error).code === Code.NotFound;
  return (
    <div className="empty card">
      <p role="alert">{notFound ? missing : `Could not load ${what}: ${errorText(error)}`}</p>
      {children}
    </div>
  );
}
