import { useQuery } from "@tanstack/react-query";
import { useSearch } from "@tanstack/react-router";

import { BrandMark } from "../components/BrandMark";
import "./LoginPage.css";
import { useTitle } from "../lib/useTitle";
import { at } from "../lib/base";

export function LoginPage() {
  useTitle("Log in");
  const search = useSearch({ strict: false }) as { back?: string };
  // back is a path of the UI router; the server needs the path on the host, with the base path.
  const back = at(search.back && search.back.startsWith("/") ? search.back : "/");
  // The button names Microsoft when the server uses Entra ID. Without an answer, it stays generic.
  const { data: provider } = useQuery({
    queryKey: ["provider"],
    queryFn: async () => ((await (await fetch(at("auth/provider"))).json()) as { provider?: string }).provider ?? "oidc",
    retry: false,
  });
  return (
    <main className="login">
      <div className="login-panel card">
        <BrandMark size={40} />
        <h1>chronicle</h1>
        {provider === "none" ? (
          <>
            <p>This server has no web login. Use the CLI with a Kubernetes service account token:</p>
            <code className="login-command">chronicle login --token-file &lt;file&gt;</code>
          </>
        ) : (
          <>
            <p>Log in to see and run the workflows of your teams.</p>
            <a className="button button-primary login-button" href={`${at("auth/login")}?redirect=${encodeURIComponent(back)}`}>
              {provider === "microsoft" ? "Log in with Microsoft" : "Log in"}
            </a>
            <p className="login-note">Your identity provider checks who you are. Kubernetes RBAC decides what you can see and do.</p>
          </>
        )}
      </div>
    </main>
  );
}
