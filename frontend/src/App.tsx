import { RouterProvider } from "@tanstack/react-router";
import { Loader2, Terminal } from "lucide-react";
import { useEffect } from "react";

import { ToastContainer } from "./components/ToastContainer";
import { ToastProvider } from "./context/ToastContext";
import { AuthProvider, useAuth } from "./hooks/useAuth";
import { router } from "./routes/router";

function AppContent() {
  const auth = useAuth();

  // Re-run route guards (beforeLoad) whenever the auth-relevant state flips or
  // a loading pass settles. TanStack Router does not re-evaluate beforeLoad on
  // context-only updates (router.update merges context but does not invalidate
  // matches), and the isLoading unmount/remount of RouterProvider skips
  // router.load() for an already-resolved location — so without this, a
  // successful login (password or passkey) leaves the user stuck on /login
  // until a manual refresh. The isLoading gate matters for boots where
  // localStorage already marks the user authenticated: isAuthenticated never
  // flips, so the effect must re-fire when loading finishes and RouterProvider
  // is mounted with the merged (fresh) context — an invalidate fired earlier
  // would run against the router's default (logged-out) context.
  // See https://github.com/noosxe/dmanager/issues/235
  const authStateKey = `${auth.isAuthenticated}|${auth.needsSetup}`;
  useEffect(() => {
    if (!auth.isLoading) {
      router.invalidate();
    }
  }, [auth.isLoading, authStateKey]);
  if (auth.isLoading) {
    return (
      <div className="auth-container">
        <div className="auth-bg-glow-1"></div>
        <div className="auth-bg-glow-2"></div>

        <div
          className="auth-card"
          style={{
            display: "flex",
            flexDirection: "column",
            alignItems: "center",
            justifyContent: "center",
            padding: "48px 40px",
            maxWidth: "360px",
          }}
        >
          <div className="auth-logo" style={{ margin: "0 0 24px 0" }}>
            <Terminal size={28} className="auth-logo-icon" />
          </div>
          <h2
            style={{
              fontSize: "18px",
              fontWeight: 600,
              color: "var(--text-h)",
              margin: "0 0 8px 0",
              letterSpacing: "-0.2px",
            }}
          >
            Loading Console
          </h2>
          <p style={{ fontSize: "14px", color: "var(--text)", margin: "0 0 24px 0", opacity: 0.8 }}>
            Establishing secure session
          </p>
          <Loader2 size={24} className="spinner" style={{ color: "var(--accent)" }} />
        </div>
      </div>
    );
  }

  return <RouterProvider router={router} context={{ auth }} />;
}

export default function App() {
  return (
    <ToastProvider>
      <AuthProvider>
        <AppContent />
        <ToastContainer />
      </AuthProvider>
    </ToastProvider>
  );
}
