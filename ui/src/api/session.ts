// The session of the logged-in user, from /auth/me.
export interface Session {
  user: string;
  groups?: string[];
  csrfToken: string;
  expiresAt: string;
}

let current: Session | null = null;

// loadSession reads the session. It returns null when the user is not logged in.
export async function loadSession(): Promise<Session | null> {
  const resp = await fetch("/auth/me", { credentials: "same-origin" });
  if (resp.status === 401) {
    current = null;
    return null;
  }
  if (!resp.ok) {
    throw new Error(`The server answered ${resp.status} for the session.`);
  }
  current = (await resp.json()) as Session;
  return current;
}

export function csrfToken(): string | undefined {
  return current?.csrfToken;
}

// loginURL starts the login and comes back to the current page.
export function loginURL(): string {
  const back = window.location.pathname + window.location.search;
  return `/auth/login?redirect=${encodeURIComponent(back)}`;
}

export async function logout(): Promise<void> {
  await fetch("/auth/logout", {
    method: "POST",
    credentials: "same-origin",
    headers: { "X-CSRF-Token": csrfToken() ?? "" },
  });
  current = null;
}
