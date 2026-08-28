import { render, screen } from "@testing-library/react";
import { Boxes, HardDrive } from "lucide-react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { PageTabs } from "./PageTabs";

const { linkMock } = vi.hoisted(() => ({
  linkMock: vi.fn(),
}));

// Mock @tanstack/react-router — Link renders as a plain button so the
// component's class/label wiring can be asserted without a router context.
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

describe("PageTabs", () => {
  beforeEach(() => {
    linkMock.mockClear();
  });

  const tabs = [
    {
      to: "/administration/$tab",
      params: { tab: "images" },
      icon: Boxes,
      label: "Images",
      active: true,
    },
    {
      to: "/administration/$tab",
      params: { tab: "volumes" },
      icon: HardDrive,
      label: "Volumes",
      active: false,
      onClick: linkMock,
    },
  ];

  it("renders every tab item with the page-tab class and active only on the current one", () => {
    render(<PageTabs tabs={tabs} />);

    const items = screen.getAllByRole("button");
    expect(items).toHaveLength(2);
    expect(screen.getByText("Images").closest("button")).toHaveClass("page-tab");
    expect(screen.getByText("Images").closest("button")).toHaveClass("active");
    expect(screen.getByText("Volumes").closest("button")).toHaveClass("page-tab");
    expect(screen.getByText("Volumes").closest("button")).not.toHaveClass("active");
  });

  it("fires the optional onClick when a tab is clicked", () => {
    render(<PageTabs tabs={tabs} />);

    screen.getByText("Volumes").closest("button")?.click();
    expect(linkMock).toHaveBeenCalledTimes(1);
  });
});
