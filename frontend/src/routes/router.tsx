import {
  createRootRouteWithContext,
  createRoute,
  createRouter,
  Outlet,
  redirect,
} from "@tanstack/react-router";

import { Administration } from "../components/Administration";
import { AuditLogsPage } from "../components/AuditLogsPage";
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

// 6b. Audit Logs route — admin-only (defense in depth under the RPC's RoleAdmin)
const auditLogsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/audit-logs",
  beforeLoad: ({ context }) => {
    if (context.auth.needsSetup) {
      throw redirect({ to: "/setup" });
    }
    if (!context.auth.isAuthenticated) {
      throw redirect({ to: "/login" });
    }
    if (context.auth.user?.role !== "admin") {
      throw redirect({ to: "/" });
    }
  },
  component: () => (
    <DashboardLayout>
      <AuditLogsPage />
    </DashboardLayout>
  ),
});

// 7. Administration routes (read-only Docker resource inventories)
const administrationIndexRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/administration",
  beforeLoad: ({ context }) => {
    if (context.auth.needsSetup) {
      throw redirect({ to: "/setup" });
    }
    if (!context.auth.isAuthenticated) {
      throw redirect({ to: "/login" });
    }
    throw redirect({ to: "/administration/$tab", params: { tab: "images" } });
  },
});

const administrationTabRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/administration/$tab",
  beforeLoad: ({ context, params }) => {
    if (context.auth.needsSetup) {
      throw redirect({ to: "/setup" });
    }
    if (!context.auth.isAuthenticated) {
      throw redirect({ to: "/login" });
    }
    if (
      params.tab !== "images" &&
      params.tab !== "builder" &&
      params.tab !== "volumes" &&
      params.tab !== "networks"
    ) {
      throw redirect({ to: "/administration/$tab", params: { tab: "images" } });
    }
  },
  component: () => (
    <DashboardLayout>
      <Administration />
    </DashboardLayout>
  ),
});

// 8. Settings route
const settingsIndexRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/settings",
  beforeLoad: ({ context }) => {
    if (context.auth.needsSetup) {
      throw redirect({ to: "/setup" });
    }
    if (!context.auth.isAuthenticated) {
      throw redirect({ to: "/login" });
    }
    throw redirect({ to: "/settings/$tab", params: { tab: "general" } });
  },
});

const settingsTabRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/settings/$tab",
  beforeLoad: ({ context, params }) => {
    if (context.auth.needsSetup) {
      throw redirect({ to: "/setup" });
    }
    if (!context.auth.isAuthenticated) {
      throw redirect({ to: "/login" });
    }
    if (params.tab !== "general" && params.tab !== "security") {
      throw redirect({ to: "/settings/$tab", params: { tab: "general" } });
    }
  },
  component: () => (
    <DashboardLayout>
      <Settings />
    </DashboardLayout>
  ),
});

// 9. Assemble the route tree
const routeTree = rootRoute.addChildren([
  dashboardRoute,
  loginRoute,
  setupRoute,
  logsRoute,
  auditLogsRoute,
  administrationIndexRoute,
  administrationTabRoute,
  settingsIndexRoute,
  settingsTabRoute,
]);

// 10. Define the router instance
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

// 11. Register the router for TypeScript type safety
declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
