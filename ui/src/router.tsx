import type { QueryClient } from "@tanstack/react-query";
import { createRootRouteWithContext, createRoute, createRouter, redirect } from "@tanstack/react-router";

import { loadSession, type Session } from "./api/session";
import { Shell } from "./components/Shell";
import { HomePage } from "./pages/HomePage";
import { LoginPage } from "./pages/LoginPage";
import { RunPage } from "./pages/RunPage";
import { CronPage } from "./pages/CronPage";
import { CronsPage } from "./pages/CronsPage";
import { TasksPage } from "./pages/TasksPage";
import { RunsPage } from "./pages/RunsPage";
import { TemplatePage } from "./pages/TemplatePage";
import { TemplatesPage } from "./pages/TemplatesPage";

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

export interface RunsSearch {
  q?: string;
  phase?: string;
}

export const runsRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/ns/$namespace",
  component: RunsPage,
  validateSearch: (s: Record<string, unknown>): RunsSearch => ({
    q: typeof s.q === "string" && s.q !== "" ? s.q : undefined,
    phase: typeof s.phase === "string" && s.phase !== "" ? s.phase : undefined,
  }),
});

export const runRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/ns/$namespace/runs/$name",
  component: RunPage,
  validateSearch: (s: Record<string, unknown>): { node?: string } => ({ node: typeof s.node === "string" ? s.node : undefined }),
});

export const templatesRoute = createRoute({ getParentRoute: () => appRoute, path: "/ns/$namespace/templates", component: TemplatesPage });

export const templateRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/ns/$namespace/templates/$name",
  component: TemplatePage,
  validateSearch: (s: Record<string, unknown>): { cluster?: boolean } => ({ cluster: s.cluster === true || s.cluster === "true" ? true : undefined }),
});

export const cronsRoute = createRoute({ getParentRoute: () => appRoute, path: "/ns/$namespace/cron", component: CronsPage });

export const cronRoute = createRoute({ getParentRoute: () => appRoute, path: "/ns/$namespace/cron/$name", component: CronPage });

export const tasksRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/ns/$namespace/tasks",
  component: TasksPage,
  validateSearch: (s: Record<string, unknown>): { state?: string } => ({ state: typeof s.state === "string" ? s.state : undefined }),
});

const routeTree = rootRoute.addChildren([
  loginRoute,
  appRoute.addChildren([homeRoute, runsRoute, runRoute, templatesRoute, templateRoute, cronsRoute, cronRoute, tasksRoute]),
]);

export function newRouter(queryClient: QueryClient) {
  return createRouter({ routeTree, context: { queryClient }, defaultPreload: "intent" });
}

declare module "@tanstack/react-router" {
  interface Register {
    router: ReturnType<typeof newRouter>;
  }
}
