import {
  createRootRouteWithContext,
  createRoute,
  createRouter,
  Outlet,
  redirect,
} from "@tanstack/react-router";
import { ContainerGrid } from "../components/ContainerGrid";
import { DashboardLayout } from "../components/DashboardLayout";
import { Login } from "../components/Login";
import { Settings } from "../components/Settings";
import { Setup } from "../components/Setup";
import { SystemLogs } from "../components/SystemLogs";

// 1. Define routing context type
export interface RouterContext {
  auth: {
    isAuthenticated: boolean;
    needsSetup: boolean;
    user: { username: string; role: string } | null;
    logout: () => Promise<void>;
  };
}

// 2. Create the root route
export const rootRoute = createRootRouteWithContext<RouterContext>()({
  component: () => <Outlet />,
});

// 3. Protected Dashboard route (Placeholder, full layout in STORY-011)
const dashboardRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/",
  beforeLoad: ({ context }) => {
    if (context.auth.needsSetup) {
      throw redirect({ to: "/setup" });
    }
    if (!context.auth.isAuthenticated) {
      throw redirect({ to: "/login" });
    }
  },
  component: () => (
    <DashboardLayout>
      <ContainerGrid />
    </DashboardLayout>
  ),
});

// 4. Public Login route
const loginRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/login",
  beforeLoad: ({ context }) => {
    if (context.auth.needsSetup) {
      throw redirect({ to: "/setup" });
    }
    if (context.auth.isAuthenticated) {
      throw redirect({ to: "/" });
    }
  },
  component: Login,
});

// 5. Public Onboarding Setup route
const setupRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/setup",
  beforeLoad: ({ context }) => {
    if (!context.auth.needsSetup) {
      if (context.auth.isAuthenticated) {
        throw redirect({ to: "/" });
      }
      throw redirect({ to: "/login" });
    }
  },
  component: Setup,
});

// 6. Logs route
const logsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/logs",
  beforeLoad: ({ context }) => {
    if (context.auth.needsSetup) {
      throw redirect({ to: "/setup" });
    }
    if (!context.auth.isAuthenticated) {
      throw redirect({ to: "/login" });
    }
  },
  component: () => (
    <DashboardLayout>
      <SystemLogs />
    </DashboardLayout>
  ),
});

// 7. Settings route
const settingsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/settings",
  beforeLoad: ({ context }) => {
    if (context.auth.needsSetup) {
      throw redirect({ to: "/setup" });
    }
    if (!context.auth.isAuthenticated) {
      throw redirect({ to: "/login" });
    }
  },
  component: () => (
    <DashboardLayout>
      <Settings />
    </DashboardLayout>
  ),
});

// 8. Assemble the route tree
const routeTree = rootRoute.addChildren([
  dashboardRoute,
  loginRoute,
  setupRoute,
  logsRoute,
  settingsRoute,
]);

// 7. Define the router instance
export const router = createRouter({
  routeTree,
  context: {
    auth: {
      isAuthenticated: false,
      needsSetup: false,
      user: null,
      logout: async () => {},
    },
  },
});

// 8. Register the router for TypeScript type safety
declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
