import "@fontsource/overpass/400.css";
import "@fontsource/overpass/600.css";
import "@fontsource/overpass/700.css";
import "@fontsource/jetbrains-mono/400.css";
import "./styles/tokens.css";
import "./styles/base.css";
import "./styles/lists.css";

import { Code, ConnectError } from "@connectrpc/connect";
import { MutationCache, QueryCache, QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "@tanstack/react-router";
import { setNonce } from "get-nonce";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";

import { ToastProvider } from "./components/Toast";
import { newRouter } from "./router";
import { applyTheme, readTheme } from "./theme";

applyTheme(readTheme());
// The server puts a new nonce into each page and into its CSP. The styles that dialogs inject carry it.
const nonce = document.querySelector<HTMLMetaElement>('meta[name="csp-nonce"]')?.content;
if (nonce) setNonce(nonce);

// A call that the server answers with unauthenticated means that the session ended, for example after the idle
// timeout. Trying again cannot help, so the user goes to the login page and comes back to the same page.
function onCallError(err: unknown) {
  if (ConnectError.from(err).code !== Code.Unauthenticated || location.pathname === "/login") return;
  queryClient.removeQueries({ queryKey: ["session"] });
  void router.navigate({ to: "/login", search: { back: location.pathname + location.search } });
}

const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: 1, refetchOnWindowFocus: true } },
  queryCache: new QueryCache({ onError: onCallError }),
  mutationCache: new MutationCache({ onError: onCallError }),
});
const router = newRouter(queryClient);

const root = document.getElementById("root");
if (!root) {
  throw new Error("The page has no #root element.");
}
createRoot(root).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <ToastProvider>
        <RouterProvider router={router} />
      </ToastProvider>
    </QueryClientProvider>
  </StrictMode>,
);
