import { render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { Container } from "../hooks/useContainers";
import { ContainerGrid } from "./ContainerGrid";

const { state } = vi.hoisted(() => ({ state: { containers: [] as unknown[] } }));

vi.mock("../hooks/useAuth", () => ({ useAuth: () => ({ user: { role: "admin" } }) }));
vi.mock("../hooks/useContainers", () => ({
  useContainers: () => ({
    containers: state.containers,
    isLoading: false,
    error: null,
    actionLoading: {},
  }),
}));
vi.mock("./LogsDrawer", () => ({ LogsDrawer: () => null }));

const container = (id: string): Container => ({
  id,
  name: "compar",
  image: "registry.example.com/compar:latest",
  imageId: "",
  state: "running",
  autoUpdate: false,
  updateAvailable: false,
  latestImageDigest: "",
  lastCheckedAt: "",
  lastUpdatedAt: "",
  externallyManaged: false,
  managedBy: "",
});

describe("ContainerGrid cards", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  // Two containers share a name for a moment while one is being replaced.
  it("keys cards by container ID, so same-named cards leave nothing behind", () => {
    const error = vi.spyOn(console, "error");
    state.containers = [container("old-id"), container("new-id")];
    const { rerender } = render(<ContainerGrid />);

    state.containers = [container("new-id")];
    rerender(<ContainerGrid />);

    expect(screen.getAllByRole("heading", { name: "compar" })).toHaveLength(1);
    expect(screen.queryByText("old-id")).not.toBeInTheDocument();
    expect(error).not.toHaveBeenCalled();
  });
});
