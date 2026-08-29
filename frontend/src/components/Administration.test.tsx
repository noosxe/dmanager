import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { adminClient } from "../client";
import type {
  DeleteImageResponse,
  Image,
  ListImagesResponse,
  ListNetworksResponse,
  ListVolumesResponse,
  Network,
  Volume,
  BuildCacheRecord,
  GetBuildCacheStatsResponse,
  GetVolumeUsageResponse,
} from "../gen/proto/dmanager/v1/admin_pb";
import { Administration } from "./Administration";

const { mockUseAuth, useParamsMock } = vi.hoisted(() => ({
  mockUseAuth: vi.fn(),
  useParamsMock: vi.fn(),
}));

const mockToast = {
  success: vi.fn(),
  error: vi.fn(),
  info: vi.fn(),
  warning: vi.fn(),
};

// Mock the toast context — the delete mutation reports via toasts.
vi.mock("../context/ToastContext", () => ({
  useToast: () => mockToast,
}));

// Mock useAuth — Administration gates the delete action on the admin role.
vi.mock("../hooks/useAuth", () => ({
  useAuth: () => mockUseAuth(),
}));

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
    deleteImage: vi.fn(),
    pruneImages: vi.fn(),
    getBuildCacheStats: vi.fn(),
    pruneBuildCache: vi.fn(),
    listBuildCacheRecords: vi.fn(),
    pruneBuildCacheRecord: vi.fn(),
    getVolumeUsage: vi.fn(),
    pruneVolumes: vi.fn(),
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

// Fixture variant where the first image is unused (count 0) and deletable.
const deletableImages: Image[] = [
  {
    id: "sha256:bbb222333444555666777888999000111222333444555666777888999000111",
    repoTags: ["busybox:1.36"],
    createdUnix: twoHoursAgoUnix,
    sizeBytes: 4194304n,
    containersCount: 0n,
  } as unknown as Image,
  ...mockImages,
];

const stubDeletableImages = () =>
  vi.mocked(adminClient.listImages).mockResolvedValue({
    images: deletableImages,
    $typeName: "dmanager.v1.ListImagesResponse",
  } as unknown as ListImagesResponse);

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
  // Never let fake timers leak between tests (waitFor would hang forever).
  afterEach(() => {
    vi.useRealTimers();
  });

  beforeEach(() => {
    vi.clearAllMocks();
    mockUseAuth.mockReturnValue({ user: { username: "admin", role: "admin" } });
    vi.mocked(adminClient.deleteImage).mockResolvedValue({
      $typeName: "dmanager.v1.DeleteImageResponse",
    } as unknown as DeleteImageResponse);
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
    // Em dashes: dangling in-use cell + both inert Actions cells.
    expect(screen.getAllByText("<none>")).toHaveLength(2);
    expect(screen.getAllByText("—")).toHaveLength(3);
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
    expect(screen.getAllByText("—")).toHaveLength(4); // created + labels + 2x unmeasured Size
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
    expect(screen.getAllByText("0")).toHaveLength(3); // Images, Unused, Dangling
  });

  it("shows an error banner when the backend is unreachable", async () => {
    vi.mocked(adminClient.listImages).mockRejectedValue(new Error("daemon down"));

    render(<Administration />);

    expect(
      await screen.findByText("Unable to connect to the Docker monitor backend."),
    ).toBeInTheDocument();

    // Stat cards fall back to -- placeholders on error.
    expect(screen.getAllByText("--")).toHaveLength(5); // one per stat card
  });

  it("re-fetches resources when the Refresh button is clicked", async () => {
    render(<Administration />);

    await waitFor(() => {
      expect(screen.getByText("nginx")).toBeInTheDocument();
    });
    expect(adminClient.listImages).toHaveBeenCalledTimes(1);

    fireEvent.click(screen.getByRole("button", { name: /sync now/i }));

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

    // Unused: 0 — the tagless image has an unknown (-1) count, so it stays
    // conservatively out of the usage-derived cards. Dangling: 0 — the same
    // image is untagged AND unused-required (#203): unknown usage excludes it.
    const unusedCard = screen.getByText("Unused").closest(".stat-card");
    expect(unusedCard?.querySelector(".stat-value")?.textContent).toBe("0");
    const danglingCard = screen.getByText("Dangling").closest(".stat-card");
    expect(danglingCard?.querySelector(".stat-value")?.textContent).toBe("0");
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

  it("counts a tagged zero-usage image as unused but not dangling", async () => {
    vi.mocked(adminClient.listImages).mockResolvedValue({
      images: [
        {
          id: "sha256:aaa111222333444555666777888999000111222333444555666777888999000",
          repoTags: ["scratch:latest"],
          createdUnix: twoHoursAgoUnix,
          sizeBytes: 52428800n,
          containersCount: 0n,
        } as unknown as Image,
        ...mockImages,
      ],
      $typeName: "dmanager.v1.ListImagesResponse",
    } as unknown as ListImagesResponse);

    render(<Administration />);

    await waitFor(() => {
      expect(screen.getByText("scratch")).toBeInTheDocument();
    });

    // scratch is tagged with zero containers → Unused 1 (#200); the tagless
    // fixture image (unknown usage) counts as neither (#203).
    const unusedCard = screen.getByText("Unused").closest(".stat-card");
    expect(unusedCard?.querySelector(".stat-value")?.textContent).toBe("1");
    const danglingCard = screen.getByText("Dangling").closest(".stat-card");
    expect(danglingCard?.querySelector(".stat-value")?.textContent).toBe("0");
  });

  it("gates the delete action to unused images", async () => {
    stubDeletableImages();
    render(<Administration />);

    await waitFor(() => {
      expect(screen.getByText("busybox")).toBeInTheDocument();
    });

    expect(screen.getByText("Actions")).toBeInTheDocument();
    // Unused image: enabled delete button for an admin.
    const deleteBtn = screen.getByTitle("Delete image");
    expect(deleteBtn).toBeEnabled();
    // In-use (>0) and unknown (-1) rows render em dashes in the Actions cell,
    // and the dangling row's in-use cell adds a third (design: -1 shows —).
    expect(screen.getAllByText("—")).toHaveLength(3);
  });

  it("disables the delete action for viewer-role users", async () => {
    mockUseAuth.mockReturnValue({ user: { username: "viewer", role: "viewer" } });
    stubDeletableImages();
    render(<Administration />);

    await waitFor(() => {
      expect(screen.getByText("busybox")).toBeInTheDocument();
    });

    expect(screen.getByTitle("Admin required")).toBeDisabled();
  });

  it("confirms deletion through the dialog, then refreshes", async () => {
    stubDeletableImages();
    render(<Administration />);

    await waitFor(() => {
      expect(screen.getByText("busybox")).toBeInTheDocument();
    });

    // Clicking the trash opens a danger dialog naming the image.
    fireEvent.click(screen.getByTitle("Delete image"));
    expect(screen.getByRole("dialog")).toHaveAccessibleName("Delete image?");
    expect(screen.getByRole("dialog")).toHaveAccessibleDescription(
      "Image busybox:1.36 (bbb222333444) will be permanently removed from the host. This cannot be undone.",
    );
    // Danger variant focuses Cancel — Enter never pre-arms the destructive action.
    expect(document.activeElement).toBe(screen.getByRole("button", { name: "Cancel" }));

    // Confirming dispatches with force and triggers a list re-fetch.
    fireEvent.click(screen.getByRole("button", { name: "Delete" }));
    await waitFor(() => {
      expect(adminClient.deleteImage).toHaveBeenCalledTimes(1);
    });
    expect(vi.mocked(adminClient.deleteImage).mock.calls[0][0]).toEqual({
      id: "sha256:bbb222333444555666777888999000111222333444555666777888999000111",
      force: true,
    });
    await waitFor(() => {
      expect(adminClient.listImages).toHaveBeenCalledTimes(2);
    });
    expect(mockToast.success).toHaveBeenCalledWith("Image deleted successfully.");
    // The dialog closes once the outcome settles.
    await waitFor(() => {
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    });
  });

  it("dismisses the dialog without deleting", async () => {
    stubDeletableImages();
    render(<Administration />);

    await waitFor(() => {
      expect(screen.getByText("busybox")).toBeInTheDocument();
    });

    // Cancel closes without dispatching.
    fireEvent.click(screen.getByTitle("Delete image"));
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();

    // Escape also closes.
    fireEvent.click(screen.getByTitle("Delete image"));
    fireEvent.keyDown(document, { key: "Escape" });
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();

    expect(adminClient.deleteImage).not.toHaveBeenCalled();
    expect(adminClient.listImages).toHaveBeenCalledTimes(1);
  });

  it("shows a spinner while deleting and an error toast on failure", async () => {
    stubDeletableImages();
    let rejectDelete!: (err: Error) => void;
    vi.mocked(adminClient.deleteImage).mockImplementation(
      () =>
        new Promise<DeleteImageResponse>((_resolve, reject) => {
          rejectDelete = reject;
        }),
    );
    render(<Administration />);

    await waitFor(() => {
      expect(screen.getByText("busybox")).toBeInTheDocument();
    });

    fireEvent.click(screen.getByTitle("Delete image"));
    fireEvent.click(screen.getByRole("button", { name: "Delete" }));

    // Spinner on the confirm button while the deletion is in flight.
    expect(document.querySelector(".dialog-confirm-btn .spinner")).not.toBeNull();

    rejectDelete(new Error("[failed_precondition] conflict"));

    await waitFor(() => {
      expect(mockToast.error).toHaveBeenCalledWith(
        "Failed to delete image: [failed_precondition] conflict",
      );
    });
    // Failure means no refresh and no success toast.
    expect(mockToast.success).not.toHaveBeenCalled();
    expect(adminClient.listImages).toHaveBeenCalledTimes(1);
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

    expect(screen.getAllByText("--")).toHaveLength(5); // one per stat card

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
    expect(volumesTab).toHaveClass("page-tab");
    expect(volumesTab).not.toHaveClass("active");
    expect(screen.getAllByText("Images")[0].closest("button")).toHaveClass("active");
  });

  it("keeps the prune button enabled for admins while unused images exist", async () => {
    stubDeletableImages();
    render(<Administration />);

    await waitFor(() => {
      expect(screen.getByText("busybox")).toBeInTheDocument();
    });
    expect(screen.getByRole("button", { name: /prune unused/i })).toBeEnabled();
  });

  it("disables the prune button with an explanatory title when nothing is reclaimable", async () => {
    vi.mocked(adminClient.listImages).mockResolvedValue({
      images: [mockImages[0]],
      $typeName: "dmanager.v1.ListImagesResponse",
    } as unknown as ListImagesResponse);
    render(<Administration />);

    await waitFor(() => {
      expect(screen.getByText("nginx")).toBeInTheDocument();
    });
    const button = screen.getByRole("button", { name: /prune unused/i });
    expect(button).toBeDisabled();
    expect(button).toHaveAttribute("title", "No unused images to prune");
  });

  it("disables the prune button for viewer-role users", async () => {
    stubDeletableImages();
    mockUseAuth.mockReturnValue({ user: { username: "viewer", role: "viewer" } });
    render(<Administration />);

    await waitFor(() => {
      expect(screen.getByText("busybox")).toBeInTheDocument();
    });
    const unusedButton = screen.getByRole("button", { name: /prune unused/i });
    expect(unusedButton).toBeDisabled();
    expect(unusedButton).toHaveAttribute("title", "Admin role required");
    const danglingButton = screen.getByRole("button", { name: /prune dangling/i });
    expect(danglingButton).toBeDisabled();
    expect(danglingButton).toHaveAttribute("title", "Admin role required");
  });

  it("confirms the prune, reports the daemon-reclaimed bytes, and refreshes", async () => {
    stubDeletableImages();
    vi.mocked(adminClient.pruneImages).mockResolvedValue({
      imagesDeleted: [{ deleted: "", untagged: "busybox:1.36" }],
      spaceReclaimed: 4194304n,
      $typeName: "dmanager.v1.PruneImagesResponse",
    } as never);
    render(<Administration />);

    await waitFor(() => {
      expect(screen.getByText("busybox")).toBeInTheDocument();
    });

    // Arming opens a danger dialog stating the scope from the current listing.
    fireEvent.click(screen.getByRole("button", { name: /prune unused/i }));
    expect(screen.getByRole("dialog")).toHaveAccessibleName("Prune unused images?");
    expect(screen.getByRole("dialog")).toHaveAccessibleDescription(
      "Deletes all 1 unused images, reclaiming up to 4.2 MB. Images in use are never touched.",
    );
    // Danger variant focuses Cancel — Enter never pre-arms the destructive action.
    expect(document.activeElement).toBe(screen.getByRole("button", { name: "Cancel" }));

    fireEvent.click(screen.getByRole("button", { name: "Prune" }));
    await waitFor(() => {
      expect(adminClient.pruneImages).toHaveBeenCalledTimes(1);
    });
    expect(vi.mocked(adminClient.pruneImages).mock.calls[0][0]).toEqual({ danglingOnly: false });
    await waitFor(() => {
      expect(mockToast.success).toHaveBeenCalledWith("Reclaimed 4.2 MB from 1 unused image.");
    });
    await waitFor(() => {
      expect(adminClient.listImages).toHaveBeenCalledTimes(2);
    });
    await waitFor(() => {
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    });
  });

  it("reports prune failures through the error toast without a refresh", async () => {
    stubDeletableImages();
    vi.mocked(adminClient.pruneImages).mockRejectedValue(new Error("daemon went away"));
    render(<Administration />);

    await waitFor(() => {
      expect(screen.getByText("busybox")).toBeInTheDocument();
    });
    fireEvent.click(screen.getByRole("button", { name: /prune unused/i }));
    fireEvent.click(screen.getByRole("button", { name: "Prune" }));

    await waitFor(() => {
      expect(mockToast.error).toHaveBeenCalledWith("Failed to prune images: daemon went away");
    });
    expect(adminClient.listImages).toHaveBeenCalledTimes(1);
    expect(mockToast.success).not.toHaveBeenCalled();
  });

  it("excludes untagged in-use images from the Dangling card and leaves the dangling prune disabled", async () => {
    vi.mocked(adminClient.listImages).mockResolvedValue({
      images: [
        {
          id: "sha256:ccc111222333444555666777888999000111222333444555666777888999000",
          repoTags: [],
          createdUnix: twoHoursAgoUnix,
          sizeBytes: 52428800n,
          containersCount: 2n,
        } as unknown as Image,
        ...mockImages,
      ],
      $typeName: "dmanager.v1.ListImagesResponse",
    } as unknown as ListImagesResponse);
    render(<Administration />);

    await waitFor(() => {
      expect(screen.getByText("ccc111222333")).toBeInTheDocument();
    });

    // The tagless image is referenced by 2 containers: neither Dangling card
    // nor dangling-prune scope may count it (#203); unused stays 0 as well.
    const danglingCard = screen.getByText("Dangling").closest(".stat-card");
    expect(danglingCard?.querySelector(".stat-value")?.textContent).toBe("0");
    const danglingButton = screen.getByRole("button", { name: /prune dangling/i });
    expect(danglingButton).toBeDisabled();
    expect(danglingButton).toHaveAttribute("title", "No dangling images to prune");
  });

  it("confirms the dangling prune, reports daemon bytes, and refreshes", async () => {
    vi.mocked(adminClient.listImages).mockResolvedValue({
      images: [
        {
          id: "sha256:ccc111222333444555666777888999000111222333444555666777888999000",
          repoTags: [],
          createdUnix: twoHoursAgoUnix,
          sizeBytes: 52428800n,
          containersCount: 0n,
        } as unknown as Image,
        ...mockImages,
      ],
      $typeName: "dmanager.v1.ListImagesResponse",
    } as unknown as ListImagesResponse);
    vi.mocked(adminClient.pruneImages).mockResolvedValue({
      imagesDeleted: [{ deleted: "", untagged: "" }],
      spaceReclaimed: 52428800n,
      $typeName: "dmanager.v1.PruneImagesResponse",
    } as never);
    render(<Administration />);

    await waitFor(() => {
      expect(screen.getByText("ccc111222333")).toBeInTheDocument();
    });

    const danglingButton = screen.getByRole("button", { name: /prune dangling/i });
    expect(danglingButton).toBeEnabled();
    fireEvent.click(danglingButton);
    expect(screen.getByRole("dialog")).toHaveAccessibleName("Prune dangling images?");
    expect(screen.getByRole("dialog")).toHaveAccessibleDescription(
      "Deletes all 1 dangling images, reclaiming up to 52.4 MB. Tagged images are never touched.",
    );
    expect(document.activeElement).toBe(screen.getByRole("button", { name: "Cancel" }));

    fireEvent.click(screen.getByRole("button", { name: "Prune" }));
    await waitFor(() => {
      expect(adminClient.pruneImages).toHaveBeenCalledTimes(1);
    });
    expect(vi.mocked(adminClient.pruneImages).mock.calls[0][0]).toEqual({ danglingOnly: true });
    await waitFor(() => {
      expect(mockToast.success).toHaveBeenCalledWith("Reclaimed 52.4 MB from 1 dangling image.");
    });
    await waitFor(() => {
      expect(adminClient.listImages).toHaveBeenCalledTimes(2);
    });
    await waitFor(() => {
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    });
  });

  it("renders builder cache stat cards on the builder tab", async () => {
    useParamsMock.mockReturnValue({ tab: "builder" });
    vi.mocked(adminClient.getBuildCacheStats).mockResolvedValue({
      totalBytes: 33541322317n,
      reclaimableBytes: 27653977878n,
      recordCount: 634,
      activeCount: 0,
      $typeName: "dmanager.v1.GetBuildCacheStatsResponse",
    } as unknown as GetBuildCacheStatsResponse);
    render(<Administration />);

    await waitFor(() => {
      expect(screen.getByText("Build Cache")).toBeInTheDocument();
    });

    const totalCard = screen.getByText("Build Cache").closest(".stat-card");
    expect(totalCard?.querySelector(".stat-value")?.textContent).toBe("33.5 GB");
    const reclaimCard = screen.getByText("Reclaimable").closest(".stat-card");
    expect(reclaimCard?.querySelector(".stat-value")?.textContent).toBe("27.7 GB");
    const recordsCard = screen.getByText("Records").closest(".stat-card");
    expect(recordsCard?.querySelector(".stat-value")?.textContent).toBe("634");

    // Admin sees an armed prune button.
    expect(screen.getByRole("button", { name: /prune build cache/i })).toBeEnabled();
  });

  it("gates the builder prune when nothing is reclaimable or the user is a viewer", async () => {
    useParamsMock.mockReturnValue({ tab: "builder" });
    vi.mocked(adminClient.getBuildCacheStats).mockResolvedValue({
      totalBytes: 0n,
      reclaimableBytes: 0n,
      recordCount: 0,
      activeCount: 0,
      $typeName: "dmanager.v1.GetBuildCacheStatsResponse",
    } as unknown as GetBuildCacheStatsResponse);
    render(<Administration />);

    await waitFor(() => {
      expect(screen.getByText("Build Cache")).toBeInTheDocument();
    });
    const button = screen.getByRole("button", { name: /prune build cache/i });
    expect(button).toBeDisabled();
    expect(button).toHaveAttribute("title", "No build cache to prune");

    mockUseAuth.mockReturnValue({ user: { username: "viewer", role: "viewer" } });
  });

  it("gates the builder prune for viewer-role users", async () => {
    useParamsMock.mockReturnValue({ tab: "builder" });
    vi.mocked(adminClient.getBuildCacheStats).mockResolvedValue({
      totalBytes: 33541322317n,
      reclaimableBytes: 27653977878n,
      recordCount: 634,
      activeCount: 0,
      $typeName: "dmanager.v1.GetBuildCacheStatsResponse",
    } as unknown as GetBuildCacheStatsResponse);
    mockUseAuth.mockReturnValue({ user: { username: "viewer", role: "viewer" } });
    render(<Administration />);

    await waitFor(() => {
      expect(screen.getByText("Build Cache")).toBeInTheDocument();
    });
    const button = screen.getByRole("button", { name: /prune build cache/i });
    expect(button).toBeDisabled();
    expect(button).toHaveAttribute("title", "Admin role required");
  });

  it("confirms the builder prune, reports daemon bytes, and refreshes", async () => {
    useParamsMock.mockReturnValue({ tab: "builder" });
    vi.mocked(adminClient.getBuildCacheStats).mockResolvedValue({
      totalBytes: 33541322317n,
      reclaimableBytes: 27653977878n,
      recordCount: 634,
      activeCount: 0,
      $typeName: "dmanager.v1.GetBuildCacheStatsResponse",
    } as unknown as GetBuildCacheStatsResponse);
    vi.mocked(adminClient.pruneBuildCache).mockResolvedValue({
      cachesDeleted: 634,
      spaceReclaimed: 27653977878n,
      $typeName: "dmanager.v1.PruneBuildCacheResponse",
    } as never);
    render(<Administration />);

    await waitFor(() => {
      expect(screen.getByText("Build Cache")).toBeInTheDocument();
    });

    fireEvent.click(screen.getByRole("button", { name: /prune build cache/i }));
    expect(screen.getByRole("dialog")).toHaveAccessibleName("Prune build cache?");
    expect(screen.getByRole("dialog")).toHaveAccessibleDescription(
      "Deletes 634 build cache records, reclaiming up to 27.7 GB. Future image builds will be slower until the cache is rebuilt.",
    );
    // Danger variant focuses Cancel — Enter never pre-arms the destructive action.
    expect(document.activeElement).toBe(screen.getByRole("button", { name: "Cancel" }));

    fireEvent.click(screen.getByRole("button", { name: "Prune" }));
    await waitFor(() => {
      expect(adminClient.pruneBuildCache).toHaveBeenCalledTimes(1);
    });
    expect(vi.mocked(adminClient.pruneBuildCache).mock.calls[0][0]).toEqual({ all: false });
    await waitFor(() => {
      expect(mockToast.success).toHaveBeenCalledWith("Reclaimed 27.7 GB from 634 cache records.");
    });
    await waitFor(() => {
      expect(adminClient.getBuildCacheStats).toHaveBeenCalledTimes(2);
    });
    await waitFor(() => {
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    });
  });

  it("renders builder records size-sorted with in-use and shared chips", async () => {
    useParamsMock.mockReturnValue({ tab: "builder" });
    vi.mocked(adminClient.getBuildCacheStats).mockResolvedValue({
      totalBytes: 4831838308n,
      reclaimableBytes: 4831838208n,
      recordCount: 2,
      activeCount: 0,
      $typeName: "dmanager.v1.GetBuildCacheStatsResponse",
    } as unknown as GetBuildCacheStatsResponse);
    vi.mocked(adminClient.listBuildCacheRecords).mockResolvedValue({
      records: [
        {
          id: "sha256:4f8a2b1c9d0e1234",
          type: "exec.cachemount",
          description: "exec mount /bin/sh in container build",
          sizeBytes: 4831838208n,
          inUse: false,
          shared: true,
          usageCount: 7n,
          createdAt: { seconds: 1767225600n },
          lastUsedAt: { seconds: 1785585600n },
          $typeName: "dmanager.v1.BuildCacheRecord",
        } as unknown as BuildCacheRecord,
        {
          id: "sha256:busy00000000",
          type: "regular",
          description: "",
          sizeBytes: 100n,
          inUse: true,
          shared: false,
          usageCount: 3n,
          createdAt: { seconds: 1767225600n },
          $typeName: "dmanager.v1.BuildCacheRecord",
        } as unknown as BuildCacheRecord,
      ],
      $typeName: "dmanager.v1.ListBuildCacheRecordsResponse",
    } as never);
    render(<Administration />);

    await waitFor(() => {
      expect(screen.getByText("exec.cachemount")).toBeInTheDocument();
    });

    // The table renders the daemon's size-descending order verbatim.
    const rows = screen.getAllByRole("row");
    expect(rows).toHaveLength(3); // header + 2 records
    expect(rows[1].textContent).toContain("4.8 GB");
    expect(rows[1].textContent).toContain("exec mount /bin/sh in container build");
    expect(rows[1].textContent).toContain("Shared");
    expect(rows[1].textContent).not.toContain("In use");

    // The in-use row carries its chip and a disabled delete action.
    expect(rows[2].textContent).toContain("In use");
    const inUseDelete = within(rows[2]).getByRole("button");
    expect(inUseDelete).toBeDisabled();
    expect(inUseDelete).toHaveAttribute("title", "Record is in use");

    // The reclaimable row's delete action is armed for admins.
    const armedDelete = within(rows[1]).getByRole("button");
    expect(armedDelete).toBeEnabled();
    expect(armedDelete).toHaveAttribute("title", "Delete cache record");
  });

  it("gates record deletion for viewer-role users", async () => {
    useParamsMock.mockReturnValue({ tab: "builder" });
    mockUseAuth.mockReturnValue({ user: { username: "viewer", role: "viewer" } });
    vi.mocked(adminClient.listBuildCacheRecords).mockResolvedValue({
      records: [
        {
          id: "sha256:4f8a2b1c9d0e1234",
          type: "regular",
          description: "step",
          sizeBytes: 42n,
          inUse: false,
          shared: false,
          usageCount: 1n,
          createdAt: { seconds: 1767225600n },
          $typeName: "dmanager.v1.BuildCacheRecord",
        } as unknown as BuildCacheRecord,
      ],
      $typeName: "dmanager.v1.ListBuildCacheRecordsResponse",
    } as never);
    render(<Administration />);

    await waitFor(() => {
      expect(screen.getByText("regular")).toBeInTheDocument();
    });
    const row = screen.getByText("regular").closest("tr") as HTMLTableRowElement;
    const button = within(row).getByRole("button");
    expect(button).toBeDisabled();
    expect(button).toHaveAttribute("title", "Admin role required");
  });

  it("confirms per-record deletion, reports daemon truth, and refreshes", async () => {
    useParamsMock.mockReturnValue({ tab: "builder" });
    vi.mocked(adminClient.getBuildCacheStats).mockResolvedValue({
      totalBytes: 4831838308n,
      reclaimableBytes: 4831838208n,
      recordCount: 2,
      activeCount: 0,
      $typeName: "dmanager.v1.GetBuildCacheStatsResponse",
    } as unknown as GetBuildCacheStatsResponse);
    const record = {
      id: "sha256:4f8a2b1c9d0e1234",
      type: "exec.cachemount",
      description: "exec mount /bin/sh in container build",
      sizeBytes: 4831838208n,
      inUse: false,
      shared: true,
      usageCount: 7n,
      createdAt: { seconds: 1767225600n },
      lastUsedAt: { seconds: 1785585600n },
      $typeName: "dmanager.v1.BuildCacheRecord",
    } as unknown as BuildCacheRecord;
    vi.mocked(adminClient.listBuildCacheRecords).mockResolvedValue({
      records: [record],
      $typeName: "dmanager.v1.ListBuildCacheRecordsResponse",
    } as never);
    vi.mocked(adminClient.pruneBuildCacheRecord).mockResolvedValue({
      cachesDeleted: 1,
      spaceReclaimed: 4831838208n,
      $typeName: "dmanager.v1.PruneBuildCacheRecordResponse",
    } as never);
    render(<Administration />);

    await waitFor(() => {
      expect(screen.getByText("exec.cachemount")).toBeInTheDocument();
    });

    fireEvent.click(screen.getByRole("button", { name: "Delete cache record" }));
    expect(screen.getByRole("dialog")).toHaveAccessibleName("Delete cache record?");
    expect(screen.getByRole("dialog")).toHaveAccessibleDescription(
      "Deletes build cache record 4f8a2b1c9d0e (4.8 GB). Shared blob content may free less. Rebuilding this step will be slower until the cache is rebuilt.",
    );
    expect(document.activeElement).toBe(screen.getByRole("button", { name: "Cancel" }));

    fireEvent.click(screen.getByRole("button", { name: "Delete" }));
    await waitFor(() => {
      expect(adminClient.pruneBuildCacheRecord).toHaveBeenCalledTimes(1);
    });
    expect(vi.mocked(adminClient.pruneBuildCacheRecord).mock.calls[0][0]).toEqual({
      id: "sha256:4f8a2b1c9d0e1234",
    });
    await waitFor(() => {
      expect(mockToast.success).toHaveBeenCalledWith("Deleted 1 cache record, reclaimed 4.8 GB.");
    });
    // Both the record list and the stats re-fetch on settle.
    await waitFor(() => {
      expect(adminClient.listBuildCacheRecords).toHaveBeenCalledTimes(2);
      expect(adminClient.getBuildCacheStats).toHaveBeenCalledTimes(2);
    });
    await waitFor(() => {
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    });
  });

  it("keeps builder stats usable when the records fetch fails", async () => {
    useParamsMock.mockReturnValue({ tab: "builder" });
    vi.mocked(adminClient.getBuildCacheStats).mockResolvedValue({
      totalBytes: 33541322317n,
      reclaimableBytes: 27653977878n,
      recordCount: 634,
      activeCount: 0,
      $typeName: "dmanager.v1.GetBuildCacheStatsResponse",
    } as unknown as GetBuildCacheStatsResponse);
    vi.mocked(adminClient.listBuildCacheRecords).mockRejectedValue(new Error("daemon down"));
    render(<Administration />);

    // Stats cards still render their live values...
    await waitFor(() => {
      const totalCard = screen.getByText("Build Cache").closest(".stat-card");
      expect(totalCard?.querySelector(".stat-value")?.textContent).toBe("33.5 GB");
    });
    // ...while the records section alone reports the failure.
    expect(screen.getByText("Unable to Load Records")).toBeInTheDocument();
    expect(screen.queryByText("exec.cachemount")).not.toBeInTheDocument();
  });

  it("does not measure volume usage on open and renders the count card", async () => {
    useParamsMock.mockReturnValue({ tab: "volumes" });
    vi.mocked(adminClient.listVolumes).mockResolvedValue({
      volumes: mockVolumes,
      $typeName: "dmanager.v1.ListVolumesResponse",
    } as unknown as ListVolumesResponse);
    render(<Administration />);

    // The tab opens from the cheap list alone — no daemon walk.
    await waitFor(() => {
      expect(screen.getByText("tardis")).toBeInTheDocument();
    });
    expect(adminClient.listVolumes).toHaveBeenCalledTimes(1);
    expect(adminClient.getVolumeUsage).not.toHaveBeenCalled();

    // Count card from the list; sizes render placeholders until measured.
    const countCard = screen
      .getAllByText("Volumes")
      .map((el) => el.closest(".stat-card"))
      .find(Boolean);
    expect(countCard?.querySelector(".stat-value")?.textContent).toBe("2");
    expect(screen.getByText("Calculate Sizes")).toBeInTheDocument();
    expect(screen.getAllByText("—")).toHaveLength(4); // created + labels + 2x unmeasured Size
  });

  it("calculates sizes on demand and fills the Size column", async () => {
    useParamsMock.mockReturnValue({ tab: "volumes" });
    vi.mocked(adminClient.listVolumes).mockResolvedValue({
      volumes: mockVolumes,
      $typeName: "dmanager.v1.ListVolumesResponse",
    } as unknown as ListVolumesResponse);
    vi.mocked(adminClient.getVolumeUsage).mockResolvedValue({
      volumes: [
        { name: "tardis", sizeBytes: 4831838208n, refCount: 2n },
        { name: "plain", sizeBytes: -1n, refCount: 0n },
      ],
      totalSizeBytes: 4831838208n,
      reclaimableBytes: 0n,
      unusedCount: 1,
      $typeName: "dmanager.v1.GetVolumeUsageResponse",
    } as unknown as GetVolumeUsageResponse);
    render(<Administration />);

    await waitFor(() => {
      expect(screen.getByText("tardis")).toBeInTheDocument();
    });
    fireEvent.click(screen.getByRole("button", { name: "Calculate Sizes" }));

    await waitFor(() => {
      expect(adminClient.getVolumeUsage).toHaveBeenCalledTimes(1);
    });
    // Measured volume shows bytes; the walk-failed volume (-1) stays unknown.
    await waitFor(() => {
      expect(screen.getByText("4.8 GB")).toBeInTheDocument();
    });
    // plain still renders unknown (created + labels + size -1); tardis shows bytes.
    expect(screen.getAllByText("—")).toHaveLength(3);
  });

  it("gates volume reclaim for viewer-role users but allows measuring", async () => {
    useParamsMock.mockReturnValue({ tab: "volumes" });
    mockUseAuth.mockReturnValue({ user: { username: "viewer", role: "viewer" } });
    vi.mocked(adminClient.listVolumes).mockResolvedValue({
      volumes: mockVolumes,
      $typeName: "dmanager.v1.ListVolumesResponse",
    } as unknown as ListVolumesResponse);
    render(<Administration />);

    await waitFor(() => {
      expect(screen.getByText("tardis")).toBeInTheDocument();
    });
    const measure = screen.getByRole("button", { name: "Calculate Sizes" });
    expect(measure).toBeEnabled();
    const reclaim = screen.getByRole("button", { name: "Reclaim Space" });
    expect(reclaim).toBeDisabled();
    expect(reclaim).toHaveAttribute("title", "Admin role required");
  });

  it("confirms the volume prune with measured upper bounds and re-measures", async () => {
    useParamsMock.mockReturnValue({ tab: "volumes" });
    vi.mocked(adminClient.listVolumes).mockResolvedValue({
      volumes: mockVolumes,
      $typeName: "dmanager.v1.ListVolumesResponse",
    } as unknown as ListVolumesResponse);
    vi.mocked(adminClient.getVolumeUsage).mockResolvedValue({
      volumes: [
        { name: "tardis", sizeBytes: 4831838208n, refCount: 2n },
        { name: "plain", sizeBytes: -1n, refCount: 0n },
      ],
      totalSizeBytes: 4831838208n,
      reclaimableBytes: 0n,
      unusedCount: 1,
      $typeName: "dmanager.v1.GetVolumeUsageResponse",
    } as unknown as GetVolumeUsageResponse);
    vi.mocked(adminClient.pruneVolumes).mockResolvedValue({
      volumesDeleted: 1,
      names: ["plain"],
      spaceReclaimed: 300n,
      $typeName: "dmanager.v1.PruneVolumesResponse",
    } as never);
    render(<Administration />);

    await waitFor(() => {
      expect(screen.getByText("tardis")).toBeInTheDocument();
    });
    fireEvent.click(screen.getByRole("button", { name: "Calculate Sizes" }));
    await waitFor(() => {
      expect(adminClient.getVolumeUsage).toHaveBeenCalledTimes(1);
    });

    fireEvent.click(screen.getByRole("button", { name: "Reclaim Space" }));
    expect(screen.getByRole("dialog")).toHaveAccessibleName("Delete unused volumes?");
    expect(screen.getByRole("dialog")).toHaveAccessibleDescription(
      "Deletes 1 unused volume, reclaiming up to 0 B. A volume is unused only when no container — running or stopped — references it. This cannot be undone.",
    );
    expect(document.activeElement).toBe(screen.getByRole("button", { name: "Cancel" }));

    fireEvent.click(screen.getByRole("button", { name: "Delete" }));
    await waitFor(() => {
      expect(adminClient.pruneVolumes).toHaveBeenCalledWith({});
    });
    // Toast reports daemon truth, naming the removed volume.
    await waitFor(() => {
      expect(mockToast.success).toHaveBeenCalledWith(
        "Reclaimed 300.0 B from 1 unused volume (plain).",
      );
    });
    // List refreshes (cheap) and the measurement re-runs — the user opted in.
    await waitFor(() => {
      expect(adminClient.listVolumes).toHaveBeenCalledTimes(2);
      expect(adminClient.getVolumeUsage).toHaveBeenCalledTimes(2);
    });
    await waitFor(() => {
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    });
  });

  it("discloses uncalculated sizes when pruning without a measurement", async () => {
    useParamsMock.mockReturnValue({ tab: "volumes" });
    vi.mocked(adminClient.listVolumes).mockResolvedValue({
      volumes: mockVolumes,
      $typeName: "dmanager.v1.ListVolumesResponse",
    } as unknown as ListVolumesResponse);
    render(<Administration />);

    await waitFor(() => {
      expect(screen.getByText("tardis")).toBeInTheDocument();
    });
    fireEvent.click(screen.getByRole("button", { name: "Reclaim Space" }));
    expect(screen.getByRole("dialog")).toHaveAccessibleDescription(
      "Size has not been calculated yet — use Calculate Sizes for a preview. Deletes all unused volumes. A volume is unused only when no container — running or stopped — references it. This cannot be undone.",
    );
    expect(adminClient.pruneVolumes).not.toHaveBeenCalled();
  });
});
