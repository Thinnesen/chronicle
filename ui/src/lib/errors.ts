import { Code, ConnectError } from "@connectrpc/connect";

// actionError turns an API error into a sentence that tells the user what happened and what to do.
export function actionError(action: string, namespace: string, err: unknown): string {
  const e = ConnectError.from(err);
  switch (e.code) {
    case Code.PermissionDenied:
      return `You cannot ${action} in ${namespace}. Ask your platform team for the editor role.`;
    case Code.Unauthenticated:
      return "Your session has ended. Log in again.";
    case Code.FailedPrecondition:
      return e.rawMessage;
    case Code.NotFound:
      return "The run does not exist anymore.";
  }
  return `Could not ${action}: ${e.rawMessage}`;
}

// errorText is the message of an error without the Connect code, for people.
export function errorText(err: unknown): string {
  if (err instanceof ConnectError) return err.rawMessage;
  return err instanceof Error ? err.message : String(err);
}

// sentence makes a server message, which starts in lower case, a sentence of its own.
export function sentence(msg: string): string {
  const s = msg.charAt(0).toUpperCase() + msg.slice(1);
  return /[.!?]$/.test(s) ? s : `${s}.`;
}
