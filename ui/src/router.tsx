import type { QueryClient } from "@tanstack/react-query";
import { createRootRouteWithContext, createRoute, createRouter, redirect } from "@tanstack/react-router";

import { loadSession, type Session } from "./api/session";
import { Shell } from "./components/Shell";
import { HomePage } from "./pages/HomePage";
import { LoginPage } from "./pages/LoginPage";
import { RunPage } from "./pages/RunPage";
import { RunsPage } from "./pages/RunsPage";

export interface RouterContext {
  queryClient: QueryClient;
}

const rootRoute = createRootRouteWithContext<RouterContext>()();

const loginRoute = createRoute({ getParentRoute: () => rootRoute, path: "/login", component: LoginPage });

// appRoute needs a session. Without one, it sends the user to the login page and back.
const appRoute = createRoute({
  getParentRoute: () => rootRoute,
  id: "app",
  component: Shell,
  beforeLoad: async ({ context, location }): Promise<{ session: Session }> => {
    const session = await context.queryClient.fetchQuery({ queryKey: ["session"], queryFn: loadSession, staleTime: 60_000 });
    if (!session) {
      throw redirect({ to: "/login", search: { back: location.href } });
    }
    return { session };
  },
});

const homeRoute = createRoute({ getParentRoute: () => appRoute, path: "/", component: HomePage });

export const runsRoute = createRoute({ getParentRoute: () => appRoute, path: "/ns/$namespace", component: RunsPage });

export const runRoute = createRoute({ getParentRoute: () => appRoute, path: "/ns/$namespace/runs/$name", component: RunPage });

const routeTree = rootRoute.addChildren([loginRoute, appRoute.addChildren([homeRoute, runsRoute, runRoute])]);

export function newRouter(queryClient: QueryClient) {
  return createRouter({ routeTree, context: { queryClient }, defaultPreload: "intent" });
}

declare module "@tanstack/react-router" {
  interface Register {
    router: ReturnType<typeof newRouter>;
  }
}
