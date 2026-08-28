import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { adminClient } from "../client";
import type {
  Image,
  ListImagesResponse,
  ListNetworksResponse,
  ListVolumesResponse,
  Network,
  Volume,
} from "../gen/proto/dmanager/v1/admin_pb";
import { Administration } from "./Administration";

const { useParamsMock } = vi.hoisted(() => ({ useParamsMock: vi.fn() }));

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
  useParams: () => useParamsMock(),
}));

// Mock the admin client
vi.mock("../client", () => ({
  adminClient: {
    listImages: vi.fn(),
    listVolumes: vi.fn(),
    listNetworks: vi.fn(),
  },
}));

// Deterministic "2 hours ago" creation timestamp.
const twoHoursAgoUnix = BigInt(Math.floor(Date.now() / 1000) - 7200);

const mockImages: Image[] = [
  {
    id: "sha256:abc123def4567890abcdef1234567890abcdef1234567890abcdef1234567890",
    repoTags: ["nginx:latest"],
    createdUnix: twoHoursAgoUnix,
    sizeBytes: 142606336n,
    containersCount: 3n,
  } as unknown as Image,
  {
    id: "sha256:fff000fff0007890abcdef1234567890abcdef1234567890abcdef1234567890",
    repoTags: [],
    createdUnix: twoHoursAgoUnix,
    sizeBytes: 4096n,
    containersCount: -1n,
  } as unknown as Image,
];

const mockVolumes: Volume[] = [
  {
    name: "tardis",
    driver: "local",
    mountpoint: "/var/lib/docker/volumes/tardis/_data",
    createdAt: { seconds: 1717171717n },
    labels: { "com.example.some-label": "some-value" },
  } as unknown as Volume,
  {
    name: "plain",
    driver: "local",
    mountpoint: "/var/lib/docker/volumes/plain/_data",
    createdAt: undefined,
    labels: {},
  } as unknown as Volume,
];

const mockNetworks: Network[] = [
  {
    id: "7d86d31b1478e7cca9ebed7e73aa0fdeec46c5ca29497431d3007d2d9e15ed99",
    name: "bridge",
    driver: "bridge",
    scope: "local",
    internal: false,
    createdAt: { seconds: 1717171717n },
  } as unknown as Network,
  {
    id: "abc123def4567890abcdef1234567890abcdef12345678",
    name: "secure",
    driver: "bridge",
    scope: "local",
    internal: true,
    createdAt: undefined,
  } as unknown as Network,
];

describe("Administration Component", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useParamsMock.mockReturnValue({ tab: "images" });
    vi.mocked(adminClient.listImages).mockResolvedValue({
      images: mockImages,
      $typeName: "dmanager.v1.ListImagesResponse",
    } as unknown as ListImagesResponse);
    vi.mocked(adminClient.listVolumes).mockResolvedValue({
      volumes: mockVolumes,
      $typeName: "dmanager.v1.ListVolumesResponse",
    } as unknown as ListVolumesResponse);
    vi.mocked(adminClient.listNetworks).mockResolvedValue({
      networks: mockNetworks,
      $typeName: "dmanager.v1.ListNetworksResponse",
    } as unknown as ListNetworksResponse);
  });

  it("renders the images tab with columns and rows", async () => {
    render(<Administration />);

    // Tab bar
    expect(screen.getAllByText("Images")).toHaveLength(2); // tab bar + stat card label
    expect(screen.getByText("Volumes")).toBeInTheDocument();
    expect(screen.getByText("Networks")).toBeInTheDocument();

    await waitFor(() => {
      expect(screen.getByText("Repository")).toBeInTheDocument();
    });

    expect(screen.getByText("Tag")).toBeInTheDocument();
    expect(screen.getByText("Image ID")).toBeInTheDocument();
    expect(screen.getByText("Size")).toBeInTheDocument();
    expect(screen.getByText("Created")).toBeInTheDocument();
    expect(screen.getByText("In Use")).toBeInTheDocument();

    // Tagged image: repository/tag split, formatted size, container count.
    expect(screen.getByText("nginx")).toBeInTheDocument();
    expect(screen.getByText("latest")).toBeInTheDocument();
    expect(screen.getByText("abc123def456")).toBeInTheDocument();
    expect(screen.getByText("143 MB")).toBeInTheDocument(); // table cell
    expect(screen.getByText("142.6 MB")).toBeInTheDocument(); // stat card (one decimal)
    expect(screen.getByText("3")).toBeInTheDocument();
    expect(screen.getAllByText("2 hours ago")).toHaveLength(2);

    // Dangling image: <none> repository and tag, unknown container count.
    expect(screen.getAllByText("<none>")).toHaveLength(2);
    expect(screen.getByText("—")).toBeInTheDocument();
  });

  it("renders the volumes tab with columns, labels and missing dates", async () => {
    useParamsMock.mockReturnValue({ tab: "volumes" });
    render(<Administration />);

    await waitFor(() => {
      expect(screen.getByText("Volume")).toBeInTheDocument();
    });

    expect(screen.getByText("Driver")).toBeInTheDocument();
    expect(screen.getByText("Mountpoint")).toBeInTheDocument();
    expect(screen.getByText("Labels")).toBeInTheDocument();

    expect(screen.getByText("tardis")).toBeInTheDocument();
    expect(screen.getByText("/var/lib/docker/volumes/tardis/_data")).toBeInTheDocument();
    expect(screen.getByText("com.example.some-label=some-value")).toBeInTheDocument();
    // Volume without created_at or labels renders the em dash twice.
    expect(screen.getAllByText("—")).toHaveLength(2);
    // Stat cards are Images-tab only.
    expect(screen.queryByText("Total Space Used")).not.toBeInTheDocument();
  });

  it("renders the networks tab with system badge and internal flags", async () => {
    useParamsMock.mockReturnValue({ tab: "networks" });
    render(<Administration />);

    await waitFor(() => {
      expect(screen.getByText("Network")).toBeInTheDocument();
    });

    expect(screen.getByText("Driver")).toBeInTheDocument();
    expect(screen.getByText("Scope")).toBeInTheDocument();
    expect(screen.getByText("Internal")).toBeInTheDocument();

    expect(screen.getByTitle("bridge")).toBeInTheDocument();
    expect(screen.getByText("system")).toBeInTheDocument();
    expect(screen.getByText("7d86d31b1478")).toBeInTheDocument();

    // Internal flags.
    expect(screen.getByText("No")).toBeInTheDocument();
    expect(screen.getByText("Yes")).toBeInTheDocument();
  });

  it("shows an empty state when no images exist", async () => {
    vi.mocked(adminClient.listImages).mockResolvedValue({
      images: [],
      $typeName: "dmanager.v1.ListImagesResponse",
    } as unknown as ListImagesResponse);

    render(<Administration />);

    expect(await screen.findByText("No Images Found")).toBeInTheDocument();

    // Stat cards render zeros for an empty inventory.
    expect(screen.getAllByText("0 B")).toHaveLength(2);
    expect(screen.getByText("0")).toBeInTheDocument();
  });

  it("shows an error banner when the backend is unreachable", async () => {
    vi.mocked(adminClient.listImages).mockRejectedValue(new Error("daemon down"));

    render(<Administration />);

    expect(
      await screen.findByText("Unable to connect to the Docker monitor backend."),
    ).toBeInTheDocument();

    // Stat cards fall back to -- placeholders on error.
    expect(screen.getAllByText("--")).toHaveLength(3);
  });

  it("re-fetches resources when the Refresh button is clicked", async () => {
    render(<Administration />);

    await waitFor(() => {
      expect(screen.getByText("nginx")).toBeInTheDocument();
    });
    expect(adminClient.listImages).toHaveBeenCalledTimes(1);

    fireEvent.click(screen.getByRole("button", { name: /refresh/i }));

    await waitFor(() => {
      expect(adminClient.listImages).toHaveBeenCalledTimes(2);
    });
  });

  it("sorts images by size when the Size header is clicked", async () => {
    render(<Administration />);

    await waitFor(() => {
      expect(screen.getByText("nginx")).toBeInTheDocument();
    });

    // Default sort: size descending — the 143 MB image opens first.
    const rows = document.querySelectorAll(".container-table-row");
    expect(rows[0].textContent).toContain("nginx");

    // First click on Size toggles ascending: the 4.1 KB dangling image first.
    fireEvent.click(screen.getByText("Size"));
    await waitFor(() => {
      expect(document.querySelectorAll(".container-table-row")[0].textContent).toContain("<none>");
    });

    // Second click toggles back to descending.
    fireEvent.click(screen.getByText("Size"));
    await waitFor(() => {
      const sortedRows = document.querySelectorAll(".container-table-row");
      expect(sortedRows[0].textContent).toContain("nginx");
    });
  });
  it("shows summary stat cards derived from the images list", async () => {
    render(<Administration />);
    expect(screen.getByText("Total Space Used")).toBeInTheDocument();
    expect(screen.getByText("Freeable Space")).toBeInTheDocument();

    await waitFor(() => {
      expect(screen.getByText("nginx")).toBeInTheDocument();
    });

    // Total: 142606336 + 4096 bytes (card + nginx table cell).
    expect(screen.getByText("142.6 MB")).toBeInTheDocument();
    // Freeable: both fixture images are in use or unknown (-1) — nothing freeable.
    expect(screen.getByText("0 B")).toBeInTheDocument();
    expect(screen.getByText("2")).toBeInTheDocument();
  });

  it("treats unknown container counts (-1) as in use when deriving freeable space", async () => {
    vi.mocked(adminClient.listImages).mockResolvedValue({
      images: [
        {
          id: "sha256:aaa111222333444555666777888999000111222333444555666777888999000",
          repoTags: ["scratch:latest"],
          createdUnix: twoHoursAgoUnix,
          sizeBytes: 52428800n,
          containersCount: -1n,
        } as unknown as Image,
        ...mockImages,
      ],
      $typeName: "dmanager.v1.ListImagesResponse",
    } as unknown as ListImagesResponse);

    render(<Administration />);

    await waitFor(() => {
      expect(screen.getByText("scratch")).toBeInTheDocument();
    });

    // Total: 52428800 + 142606336 + 4096 bytes.
    expect(screen.getByText("195.0 MB")).toBeInTheDocument();
    // Freeable: nothing — nginx is in use, scratch is unknown, dangling is -1.
    expect(screen.getByText("0 B")).toBeInTheDocument();
    // Image count (card) and the nginx in-use cell both render 3.
    expect(screen.getAllByText("3")).toHaveLength(2);
  });

  it("shows -- stat placeholders while the images list is loading", async () => {
    let resolveList!: (value: ListImagesResponse) => void;
    vi.mocked(adminClient.listImages).mockImplementation(
      () =>
        new Promise<ListImagesResponse>((resolve) => {
          resolveList = resolve;
        }),
    );

    render(<Administration />);

    expect(screen.getAllByText("--")).toHaveLength(3);

    resolveList({
      images: mockImages,
      $typeName: "dmanager.v1.ListImagesResponse",
    } as unknown as ListImagesResponse);

    await waitFor(() => {
      expect(screen.getByText("nginx")).toBeInTheDocument();
    });
    expect(screen.getByText("142.6 MB")).toBeInTheDocument();
  });

  it("renders tab links pointing at the routed tab paths", () => {
    render(<Administration />);

    const volumesTab = screen.getByText("Volumes").closest("button");
    expect(volumesTab).toHaveClass("settings-nav-tab");
    expect(volumesTab).not.toHaveClass("active");
    expect(screen.getAllByText("Images")[0].closest("button")).toHaveClass("active");
  });
});
