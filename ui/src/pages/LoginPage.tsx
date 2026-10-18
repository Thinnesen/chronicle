import { useSearch } from "@tanstack/react-router";

import "./LoginPage.css";

export function LoginPage() {
  const search = useSearch({ strict: false }) as { back?: string };
  const back = search.back && search.back.startsWith("/") ? search.back : "/";
  return (
    <main className="login">
      <div className="login-panel">
        <svg className="login-track" viewBox="0 0 240 40" aria-hidden="true">
          <path d="M0 20h240" stroke="var(--track)" strokeWidth="3" />
          <circle cx="40" cy="20" r="7" fill="var(--lamp-proceed)" />
          <circle cx="120" cy="20" r="7" fill="var(--lamp-proceed)" />
          <circle cx="200" cy="20" r="6" fill="none" stroke="var(--lamp-running)" strokeWidth="3" />
        </svg>
        <h1>chronicle</h1>
        <p>Log in to see and run the workflows of your teams.</p>
        <a className="login-button" href={`/auth/login?redirect=${encodeURIComponent(back)}`}>
          Log in
        </a>
      </div>
    </main>
  );
}
