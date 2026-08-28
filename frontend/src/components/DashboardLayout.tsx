import { Link } from "@tanstack/react-router";
import { Activity, LayoutDashboard, LogOut, Menu, Settings } from "lucide-react";
import type React from "react";
import { useState } from "react";

import { useAuth } from "../hooks/useAuth";

interface DashboardLayoutProps {
  children: React.ReactNode;
}

export function DashboardLayout({ children }: DashboardLayoutProps) {
  const { user, logout, serverInfo } = useAuth();
  const [isSidebarOpen, setIsSidebarOpen] = useState(false);

  const toggleSidebar = () => {
    setIsSidebarOpen(!isSidebarOpen);
  };

  const closeSidebar = () => {
    setIsSidebarOpen(false);
  };

  // Safe avatar initials
  const initials = user?.username ? user.username.slice(0, 2).toUpperCase() : "US";

  return (
    <div className="dashboard-wrapper">
      {/* Mobile Top Header Bar */}
      <div className="mobile-header-bar">
        <div style={{ display: "flex", alignItems: "center", gap: "10px" }}>
          <img
            src="/logo.svg"
            className="sidebar-logo"
            alt="dmanager logo"
            style={{ width: "32px", height: "32px" }}
          />
          <span className="sidebar-brand-name" style={{ fontSize: "16px" }}>
            dmanager
          </span>
        </div>
        <button
          type="button"
          className="mobile-menu-btn"
          onClick={toggleSidebar}
          aria-label="Toggle Navigation Menu"
        >
          <Menu size={20} />
        </button>
      </div>

      {/* Sidebar Overlay for Mobile */}
      <button
        type="button"
        className={`sidebar-overlay ${isSidebarOpen ? "open" : ""}`}
        onClick={closeSidebar}
        aria-label="Close Navigation Sidebar"
      />

      {/* Sidebar Navigation */}
      <aside className={`dashboard-sidebar ${isSidebarOpen ? "open" : ""}`}>
        <div className="sidebar-top">
          <div className="sidebar-brand">
            <img src="/logo.svg" className="sidebar-logo" alt="dmanager logo" />
            <span className="sidebar-brand-name">dmanager</span>
          </div>

          <nav className="sidebar-menu">
            <Link
              to="/"
              className="menu-item"
              activeProps={{ className: "menu-item active" }}
              onClick={closeSidebar}
            >
              <LayoutDashboard size={18} />
              <span>Containers</span>
            </Link>

            <Link
              to="/logs"
              className="menu-item"
              activeProps={{ className: "menu-item active" }}
              onClick={closeSidebar}
            >
              <Activity size={18} />
              <span>System Logs</span>
            </Link>

            <Link
              to="/settings/$tab"
              params={{ tab: "general" }}
              className="menu-item"
              activeProps={{ className: "menu-item active" }}
              activeOptions={{ exact: false }}
              onClick={closeSidebar}
            >
              <Settings size={18} />
              <span>Settings</span>
            </Link>
          </nav>
        </div>

        <div className="sidebar-footer">
          <div className="server-status-pill">
            <span className="status-dot" />
            <span>Engine online</span>
          </div>

          <div className="user-profile-card">
            <div className="user-avatar">{initials}</div>
            <div className="user-info">
              <div className="user-name">{user?.username || "Guest"}</div>
              <div className="user-role">
                <span
                  style={{
                    display: "inline-block",
                    width: "5px",
                    height: "5px",
                    borderRadius: "50%",
                    background: user?.role === "admin" ? "var(--accent)" : "#9ca3af",
                    marginRight: "4px",
                  }}
                />
                <span style={{ textTransform: "capitalize" }}>{user?.role || "viewer"}</span>
              </div>
            </div>
            <button
              type="button"
              className="logout-btn-icon"
              onClick={logout}
              title="Sign Out"
              aria-label="Sign Out"
            >
              <LogOut size={16} />
            </button>
          </div>

          {serverInfo && (
            <div
              className="sidebar-version"
              title={
                serverInfo.commit !== "none"
                  ? `commit ${serverInfo.commit} · built ${serverInfo.buildDate}`
                  : undefined
              }
            >
              {serverInfo.version}
            </div>
          )}
        </div>
      </aside>

      {/* Main Content Area */}
      <main className="main-container">{children}</main>
    </div>
  );
}
