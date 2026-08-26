import { createContext, useContext, useEffect, useState, type ReactNode } from "react";
import { ConnectError, Code } from "@connectrpc/connect";
import { get as webauthnGet } from "@github/webauthn-json";
import { authClient } from "../client";

export interface User {
  username: string;
  role: string;
}

export interface AuthContextType {
  user: User | null;
  isAuthenticated: boolean;
  isLoading: boolean;
  needsSetup: boolean;
  passkeyLoginEnabled: boolean;
  login: (username: string, password: string, rememberMe?: boolean) => Promise<void>;
  loginWithPasskey: (rememberMe?: boolean) => Promise<void>;
  setupAdmin: (username: string, password: string) => Promise<void>;
  logout: () => Promise<void>;
  checkAuth: () => Promise<void>;
}

const AuthContext = createContext<AuthContextType | undefined>(undefined);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null>(() => {
    const stored = localStorage.getItem("dmanager_user");
    return stored ? JSON.parse(stored) : null;
  });
  const [isAuthenticated, setIsAuthenticated] = useState<boolean>(() => {
    return !!localStorage.getItem("dmanager_token");
  });
  const [isLoading, setIsLoading] = useState<boolean>(true);
  const [needsSetup, setNeedsSetup] = useState<boolean>(false);
  const [passkeyLoginEnabled, setPasskeyLoginEnabled] = useState<boolean>(false);

  const checkAuth = async () => {
    try {
      const response = await authClient.getMe({});
      if (response.username) {
        const loggedUser = { username: response.username, role: response.role };
        setUser(loggedUser);
        setIsAuthenticated(true);
        localStorage.setItem("dmanager_user", JSON.stringify(loggedUser));
        localStorage.setItem("dmanager_token", "session_active");
      }
      setNeedsSetup(false);
    } catch (error: unknown) {
      // If unauthenticated (ConnectRPC code 16 / Unauthenticated)
      const isUnauthenticated =
        error instanceof ConnectError && error.code === Code.Unauthenticated;
      const messageIncludesUnauthenticated =
        error instanceof Error && error.message.toLowerCase().includes("unauthenticated");

      if (isUnauthenticated || messageIncludesUnauthenticated) {
        setUser(null);
        setIsAuthenticated(false);
        localStorage.removeItem("dmanager_user");
        localStorage.removeItem("dmanager_token");

        // Check if setup is needed and passkey status
        try {
          const status = await authClient.getServerStatus({});
          setNeedsSetup(status.needsSetup);
          setPasskeyLoginEnabled(status.passkeyLoginEnabled);
        } catch (statusErr) {
          console.error("Failed to check server setup status:", statusErr);
        }
      } else {
        console.error("Failed to fetch user profiles:", error);
      }
    } finally {
      setIsLoading(false);
    }
  };

  useEffect(() => {
    checkAuth();
  }, []);

  const login = async (username: string, password: string, rememberMe = false) => {
    setIsLoading(true);
    try {
      const response = await authClient.login({ username, password, rememberMe });
      const loggedUser = { username: response.username, role: response.role };
      setUser(loggedUser);
      setIsAuthenticated(true);
      setNeedsSetup(false);
      localStorage.setItem("dmanager_user", JSON.stringify(loggedUser));
      localStorage.setItem("dmanager_token", "session_active");
    } catch (error) {
      setUser(null);
      setIsAuthenticated(false);
      localStorage.removeItem("dmanager_user");
      localStorage.removeItem("dmanager_token");
      throw error;
    } finally {
      setIsLoading(false);
    }
  };

  const loginWithPasskey = async (rememberMe = false) => {
    setIsLoading(true);
    try {
      const beginResp = await authClient.beginPasskeyLogin({});
      const options = JSON.parse(beginResp.optionsJson);
      const credential = await webauthnGet({ publicKey: options });
      const response = await authClient.finishPasskeyLogin({
        responseJson: JSON.stringify(credential),
        rememberMe,
      });
      const loggedUser = { username: response.username, role: response.role };
      setUser(loggedUser);
      setIsAuthenticated(true);
      setNeedsSetup(false);
      localStorage.setItem("dmanager_user", JSON.stringify(loggedUser));
      localStorage.setItem("dmanager_token", "session_active");
    } catch (error) {
      setUser(null);
      setIsAuthenticated(false);
      localStorage.removeItem("dmanager_user");
      localStorage.removeItem("dmanager_token");
      throw error;
    } finally {
      setIsLoading(false);
    }
  };

  const setupAdmin = async (username: string, password: string) => {
    setIsLoading(true);
    try {
      // 1. Create the admin account
      await authClient.setupAdmin({ username, password });
      setNeedsSetup(false);

      // 2. Automatically log in the user with the new credentials
      const response = await authClient.login({ username, password });
      const loggedUser = { username: response.username, role: response.role };
      setUser(loggedUser);
      setIsAuthenticated(true);
      localStorage.setItem("dmanager_user", JSON.stringify(loggedUser));
      localStorage.setItem("dmanager_token", "session_active");
    } catch (error) {
      setUser(null);
      setIsAuthenticated(false);
      localStorage.removeItem("dmanager_user");
      localStorage.removeItem("dmanager_token");
      throw error;
    } finally {
      setIsLoading(false);
    }
  };

  const logout = async () => {
    setIsLoading(true);
    try {
      await authClient.logout({});
    } catch (error) {
      console.error("Logout RPC error:", error);
    } finally {
      setUser(null);
      setIsAuthenticated(false);
      setNeedsSetup(false);
      localStorage.removeItem("dmanager_user");
      localStorage.removeItem("dmanager_token");
      setIsLoading(false);
    }
  };

  return (
    <AuthContext.Provider
      value={{
        user,
        isAuthenticated,
        isLoading,
        needsSetup,
        passkeyLoginEnabled,
        login,
        loginWithPasskey,
        setupAdmin,
        logout,
        checkAuth,
      }}
    >
      {children}
    </AuthContext.Provider>
  );
}

export function useAuth() {
  const context = useContext(AuthContext);
  if (context === undefined) {
    throw new Error("useAuth must be used within an AuthProvider");
  }
  return context;
}
