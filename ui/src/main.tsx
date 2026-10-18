import "@fontsource/overpass/400.css";
import "@fontsource/overpass/700.css";
import "@fontsource/jetbrains-mono/400.css";
import "./styles/tokens.css";
import "./styles/base.css";

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "@tanstack/react-router";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";

import { newRouter } from "./router";
import { applyTheme, readTheme } from "./theme";

applyTheme(readTheme());

const queryClient = new QueryClient({ defaultOptions: { queries: { retry: 1, refetchOnWindowFocus: true } } });
const router = newRouter(queryClient);

const root = document.getElementById("root");
if (!root) {
  throw new Error("The page has no #root element.");
}
createRoot(root).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  </StrictMode>,
);
