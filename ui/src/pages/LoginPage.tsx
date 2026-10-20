import { useQuery } from "@tanstack/react-query";
import { useSearch } from "@tanstack/react-router";

import { BrandMark } from "../components/BrandMark";
import "./LoginPage.css";
import { useTitle } from "../lib/useTitle";

export function LoginPage() {
  useTitle("Log in");
  const search = useSearch({ strict: false }) as { back?: string };
  const back = search.back && search.back.startsWith("/") ? search.back : "/";
  // The button names Microsoft when the server uses Entra ID. Without an answer, it stays generic.
  const { data: provider } = useQuery({
    queryKey: ["provider"],
    queryFn: async () => ((await (await fetch("/auth/provider")).json()) as { provider?: string }).provider ?? "oidc",
    retry: false,
  });
  return (
    <main className="login">
      <div className="login-panel card">
        <BrandMark size={40} />
        <h1>chronicle</h1>
        <p>Log in to see and run the workflows of your teams.</p>
        <a className="button button-primary login-button" href={`/auth/login?redirect=${encodeURIComponent(back)}`}>
          {provider === "microsoft" ? "Log in with Microsoft" : "Log in"}
        </a>
        <p className="login-note">Your identity provider checks who you are. Kubernetes RBAC decides what you can see and do.</p>
      </div>
    </main>
  );
}
