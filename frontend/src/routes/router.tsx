import {
  createRootRouteWithContext,
  createRoute,
  createRouter,
  Outlet,
  redirect,
} from "@tanstack/react-router";
import { LogOut, Shield, Terminal, User } from "lucide-react";
import { Login } from "../components/Login";
import { Setup } from "../components/Setup";
import { useAuth } from "../hooks/useAuth";

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
  component: () => {
    const { user, logout } = useAuth();

    return (
      <div className="auth-container">
        <div className="auth-bg-glow-1"></div>
        <div className="auth-bg-glow-2"></div>

        <div className="auth-card" style={{ maxWidth: "500px" }}>
          <div className="auth-header">
            <div className="auth-logo">
              <Terminal size={28} className="auth-logo-icon" />
            </div>
            <h1>dmanager</h1>
            <p className="auth-subtitle">Welcome back to management console</p>
          </div>

          <div
            style={{ margin: "24px 0", textAlign: "left" }}
            className="dashboard-placeholder-info"
          >
            <div
              style={{
                display: "flex",
                alignItems: "center",
                gap: "12px",
                marginBottom: "16px",
                padding: "12px",
                background: "rgba(255, 255, 255, 0.03)",
                borderRadius: "8px",
                border: "1px solid var(--border)",
              }}
            >
              <User size={20} style={{ color: "var(--accent)" }} />
              <div>
                <div
                  style={{
                    fontSize: "12px",
                    color: "var(--text)",
                    textTransform: "uppercase",
                    letterSpacing: "0.5px",
                  }}
                >
                  User
                </div>
                <div style={{ fontWeight: 500, color: "var(--text-h)" }}>{user?.username}</div>
              </div>
            </div>

            <div
              style={{
                display: "flex",
                alignItems: "center",
                gap: "12px",
                padding: "12px",
                background: "rgba(255, 255, 255, 0.03)",
                borderRadius: "8px",
                border: "1px solid var(--border)",
              }}
            >
              <Shield size={20} style={{ color: "var(--accent)" }} />
              <div>
                <div
                  style={{
                    fontSize: "12px",
                    color: "var(--text)",
                    textTransform: "uppercase",
                    letterSpacing: "0.5px",
                  }}
                >
                  Role
                </div>
                <div style={{ fontWeight: 500, color: "var(--text-h)" }}>{user?.role}</div>
              </div>
            </div>
          </div>

          <button
            type="button"
            onClick={logout}
            className="auth-submit-btn"
            style={{
              background: "rgba(239, 68, 68, 0.15)",
              color: "#ef4444",
              border: "1px solid rgba(239, 68, 68, 0.3)",
            }}
          >
            <LogOut size={18} />
            <span>Sign Out</span>
          </button>
        </div>
      </div>
    );
  },
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

// 6. Assemble the route tree
const routeTree = rootRoute.addChildren([dashboardRoute, loginRoute, setupRoute]);

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
