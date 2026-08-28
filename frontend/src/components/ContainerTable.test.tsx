import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { Container } from "../hooks/useContainers";
import { ContainerTable } from "./ContainerTable";

const mockContainers: Container[] = [
  {
    id: "abcdef1234567890abcdef1234567890",
    name: "test-container-1",
    image: "nginx:latest",
    imageId: "sha256:11111111111111111111111111111111",
    state: "running",
    autoUpdate: true,
    updateAvailable: false,
    latestImageDigest: "sha256:nginx123",
    lastCheckedAt: "2026-07-14T00:00:00Z",
    lastUpdatedAt: "2026-07-14T00:00:00Z",
  },
  {
    id: "9876543210fedcbafedcba9876543210",
    name: "test-container-2",
    image: "redis:alpine",
    imageId: "sha256:22222222222222222222222222222222",
    state: "exited",
    autoUpdate: false,
    updateAvailable: true,
    latestImageDigest: "sha256:redis123",
    lastCheckedAt: "2026-07-13T12:00:00Z",
    lastUpdatedAt: "2026-07-13T12:00:00Z",
  },
];

describe("ContainerTable Component", () => {
  const defaultProps = {
    containers: mockContainers,
    isAdmin: true,
    actionLoading: {},
    startContainer: vi.fn(),
    stopContainer: vi.fn(),
    upgradeContainer: vi.fn(),
    setContainerAutoUpdate: vi.fn(),
    checkContainerUpdates: vi.fn(),
    onViewLogs: vi.fn(),
    formatDate: (iso: string) => iso,
  };

  it("renders the table headers and container rows correctly", () => {
    render(<ContainerTable {...defaultProps} />);

    // Header checks
    expect(screen.getByText("Name")).toBeInTheDocument();
    expect(screen.getByText("Status")).toBeInTheDocument();
    expect(screen.getByText("Image & Tag")).toBeInTheDocument();
    expect(screen.getByText("Container ID")).toBeInTheDocument();
    expect(screen.getByText("Auto Updates")).toBeInTheDocument();
    expect(screen.getByText("Last Checked")).toBeInTheDocument();
    expect(screen.getByText("Actions")).toBeInTheDocument();

    // Container 1 checks
    expect(screen.getByText("test-container-1")).toBeInTheDocument();
    expect(screen.getByText("nginx:latest")).toBeInTheDocument();
    expect(screen.getByText("abcdef123456")).toBeInTheDocument(); // Short ID
    expect(screen.getByText("running")).toBeInTheDocument();

    // Container 2 checks
    expect(screen.getByText("test-container-2")).toBeInTheDocument();
    expect(screen.getByText("redis:alpine")).toBeInTheDocument();
    expect(screen.getByText("9876543210fe")).toBeInTheDocument(); // Short ID
    expect(screen.getByText("exited")).toBeInTheDocument();
    expect(screen.getByText("Update Ready")).toBeInTheDocument(); // Update indicator
  });

  it("calls action callbacks on click", () => {
    render(<ContainerTable {...defaultProps} />);

    // Stop container for running container
    const stopBtn = screen.getByTitle("Stop Container");
    fireEvent.click(stopBtn);
    expect(defaultProps.stopContainer).toHaveBeenCalledWith(mockContainers[0].id);

    // Start container for stopped container
    const startBtn = screen.getByTitle("Start Container");
    fireEvent.click(startBtn);
    expect(defaultProps.startContainer).toHaveBeenCalledWith(mockContainers[1].id);

    // Upgrade container for container with updates
    const upgradeBtn = screen.getByTitle("Upgrade Container");
    fireEvent.click(upgradeBtn);
    expect(defaultProps.upgradeContainer).toHaveBeenCalledWith(mockContainers[1].id);

    // Check updates manually
    const checkBtns = screen.getAllByTitle("Check Updates");
    fireEvent.click(checkBtns[0]);
    expect(defaultProps.checkContainerUpdates).toHaveBeenCalledWith(mockContainers[0].id);

    // View logs
    const logsBtns = screen.getAllByTitle("View Logs");
    fireEvent.click(logsBtns[0]);
    expect(defaultProps.onViewLogs).toHaveBeenCalledWith(
      mockContainers[0].id,
      mockContainers[0].name,
    );
  });

  it("disables admin actions if isAdmin is false", () => {
    const nonAdminProps = {
      ...defaultProps,
      isAdmin: false,
    };
    render(<ContainerTable {...nonAdminProps} />);

    // Auto-update toggles should be disabled
    const toggleBtns = screen.getAllByTitle("Admin required");
    expect(toggleBtns).toHaveLength(2);
    for (const btn of toggleBtns) {
      expect(btn).toBeDisabled();
    }

    // Stop button is disabled
    const stopBtn = screen.getByTitle("Stop Container");
    expect(stopBtn).toBeDisabled();

    // Start button is disabled
    const startBtn = screen.getByTitle("Start Container");
    expect(startBtn).toBeDisabled();

    // Upgrade button is disabled
    const upgradeBtn = screen.getByTitle("Upgrade Container");
    expect(upgradeBtn).toBeDisabled();
  });

  it("renders empty state when no containers are present", () => {
    render(<ContainerTable {...defaultProps} containers={[]} />);

    expect(screen.getByText("No Containers Found")).toBeInTheDocument();
  });
});
