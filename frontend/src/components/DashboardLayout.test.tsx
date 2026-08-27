import { render, screen } from "@testing-library/react";
import type React from "react";
import { describe, expect, it, vi } from "vitest";
import { DashboardLayout } from "./DashboardLayout";

// Mock @tanstack/react-router
vi.mock("@tanstack/react-router", () => ({
  Link: ({
    children,
    className,
    onClick,
  }: {
    children?: React.ReactNode;
    className?: string;
    onClick?: (e: React.MouseEvent) => void;
  }) => (
    <button
      type="button"
      className={className}
      onClick={(e) => {
        e.preventDefault();
        if (onClick) onClick(e);
      }}
    >
      {children}
    </button>
  ),
}));

const mockUseAuth = vi.fn();

vi.mock("../hooks/useAuth", () => ({
  useAuth: () => mockUseAuth(),
}));

function renderLayout(serverInfo: { version: string; commit: string; buildDate: string } | null) {
  mockUseAuth.mockReturnValue({
    user: { username: "admin", role: "admin" },
    logout: vi.fn(),
    serverInfo,
  });
  return render(
    <DashboardLayout>
      <div>main content</div>
    </DashboardLayout>,
  );
}

describe("DashboardLayout sidebar version", () => {
  it("renders the server version at the bottom of the sidebar", () => {
    renderLayout({ version: "v1.2.3", commit: "abc123", buildDate: "2026-08-27T16:00:00Z" });

    const versionEl = screen.getByText("v1.2.3");
    expect(versionEl).toHaveClass("sidebar-version");
    expect(versionEl.getAttribute("title")).toBe("commit abc123 · built 2026-08-27T16:00:00Z");

    // Must come after the user profile card in the sidebar footer.
    const profileCard = document.querySelector(".user-profile-card");
    expect(profileCard).not.toBeNull();
    expect(profileCard?.compareDocumentPosition(versionEl as Node)).toBe(
      Node.DOCUMENT_POSITION_FOLLOWING,
    );
  });

  it("omits the build tooltip for dev builds without commit metadata", () => {
    renderLayout({ version: "dev", commit: "none", buildDate: "unknown" });

    const versionEl = screen.getByText("dev");
    expect(versionEl.getAttribute("title")).toBeNull();
  });

  it("hides the version until server status has loaded", () => {
    renderLayout(null);
    expect(document.querySelector(".sidebar-version")).toBeNull();
  });
});
