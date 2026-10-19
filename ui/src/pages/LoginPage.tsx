import { useSearch } from "@tanstack/react-router";

import { BrandMark } from "../components/BrandMark";
import "./LoginPage.css";

export function LoginPage() {
  const search = useSearch({ strict: false }) as { back?: string };
  const back = search.back && search.back.startsWith("/") ? search.back : "/";
  return (
    <main className="login">
      <div className="login-panel card">
        <BrandMark size={40} />
        <h1>chronicle</h1>
        <p>Log in to see and run the workflows of your teams.</p>
        <a className="button button-primary login-button" href={`/auth/login?redirect=${encodeURIComponent(back)}`}>
          Log in
        </a>
        <p className="login-note">Your identity provider checks who you are. Kubernetes RBAC decides what you can see and do.</p>
      </div>
    </main>
  );
}
