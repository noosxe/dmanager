// Fixture data for the mock preview (`pnpm --dir frontend dev:mock`).
// Everything here is placeholder data: no real hostnames, users or tailnets.
import { timestampFromDate } from "@bufbuild/protobuf/wkt";

const MINUTE = 60_000;
const ago = (ms: number) => new Date(Date.now() - ms);
const agoIso = (ms: number) => ago(ms).toISOString();
const ts = (ms: number) => timestampFromDate(ago(ms));

export type Scenario = "admin" | "login" | "setup" | "empty";
export type TailscaleVariant = "running" | "starting" | "disabled" | "failed";

export const SCENARIOS: Scenario[] = ["admin", "login", "setup", "empty"];
export const TAILSCALE_VARIANTS: TailscaleVariant[] = ["running", "starting", "disabled", "failed"];

/** Bare-key selector the mock server pretends to be configured with (a label-present match). */
export const EXTERNAL_SELECTOR = "com.example.stack";

export interface MockContainer {
  id: string;
  name: string;
  image: string;
  imageId: string;
  state: string;
  autoUpdate: boolean;
  updateAvailable: boolean;
  latestImageDigest: string;
  lastCheckedAt: string;
  lastUpdatedAt: string;
  labels: Record<string, string>;
}

const digest = (n: number) => `sha256:${n.toString(16).padStart(64, "0")}`;

export function containerFixtures(): MockContainer[] {
  const base = {
    imageId: digest(1),
    latestImageDigest: "",
    lastCheckedAt: agoIso(20 * MINUTE),
    lastUpdatedAt: agoIso(3 * 24 * 60 * MINUTE),
    labels: {},
  };
  return [
    {
      ...base,
      id: "a1",
      name: "app-1",
      image: "example.com/team/app-1:1.4.2",
      state: "running",
      autoUpdate: false,
      updateAvailable: false,
    },
    {
      ...base,
      id: "a2",
      name: "app-2-worker",
      image: "example.com/team/app-2:latest",
      state: "running",
      autoUpdate: true,
      updateAvailable: false,
    },
    {
      ...base,
      id: "a3",
      name: "db-1",
      image: "postgres:17",
      state: "running",
      autoUpdate: false,
      updateAvailable: true,
      latestImageDigest: digest(2),
    },
    {
      ...base,
      id: "a4",
      name: "cache-1",
      image: "redis:7-alpine",
      state: "exited",
      autoUpdate: false,
      updateAvailable: false,
    },
    {
      ...base,
      id: "a5",
      name: "proxy-1",
      image: "example.com/proxy:2.0",
      state: "running",
      autoUpdate: true,
      updateAvailable: true,
      latestImageDigest: digest(3),
    },
    {
      ...base,
      id: "a6",
      name: "stack-web-1",
      image: "example.com/team/web:5.1",
      state: "running",
      autoUpdate: false,
      updateAvailable: true,
      latestImageDigest: digest(4),
      labels: { "dmanager.managed-by": "compose-stack" },
    },
    {
      ...base,
      id: "a7",
      name: "stack-api-1",
      image: "example.com/team/api:5.1",
      state: "exited",
      autoUpdate: false,
      updateAvailable: false,
      labels: { "com.example.stack": "billing" },
    },
    {
      ...base,
      id: "a8",
      name: "a-very-long-container-name-that-keeps-going-and-going-and-would-overflow-a-narrow-card-1",
      image:
        "registry.example.com/some-organisation/some-team/a-deeply-nested-repository-name/with-many-path-segments:2026.11.20-build.123456789-linux-amd64-extra-long-tag",
      state: "running",
      autoUpdate: true,
      updateAvailable: false,
    },
    {
      ...base,
      id: "a9",
      name: "another-extremely-long-externally-managed-container-name-for-badge-wrapping-2",
      image: "example.com/team/long-managed-image-name-for-layout-checks:1.0.0",
      state: "running",
      autoUpdate: false,
      updateAvailable: true,
      latestImageDigest: digest(5),
      labels: { "dmanager.managed-by": "a-tool-with-a-rather-long-name" },
    },
  ];
}

export const ADMIN_USER = { userId: 1n, username: "admin", role: "admin" };

export const SERVER_VERSION = { version: "0.0.0-mock", commit: "0000000", buildDate: "2026-01-01" };

export const LOG_LINES: { stream: string; line: string }[] = [
  { stream: "stdout", line: "starting app on :8080" },
  { stream: "stdout", line: "connected to db-1:5432" },
  { stream: "stderr", line: "WARN slow query took 1.2s" },
  { stream: "stdout", line: "GET /healthz 200 1ms" },
  { stream: "stderr", line: "ERROR upstream timeout while calling https://example.com/api" },
];

export function tailscaleFixture(variant: TailscaleVariant) {
  const common = {
    port: 443,
    httpsEnabled: true,
    certDomains: [] as string[],
    ips: [] as string[],
    hostname: "",
    dnsName: "",
    error: "",
    backendState: "",
  };
  switch (variant) {
    case "running":
      return {
        ...common,
        enabled: true,
        state: "running",
        backendState: "Running",
        hostname: "dmanager",
        dnsName: "dmanager.<tailnet>.ts.net",
        ips: ["100.64.0.1", "fd7a:115c:a1e0::1"],
        certDomains: ["dmanager.<tailnet>.ts.net"],
        keyExpiry: ts(-90 * 24 * 60 * MINUTE),
      };
    case "starting":
      return {
        ...common,
        enabled: true,
        state: "starting",
        backendState: "NeedsLogin",
        hostname: "dmanager",
      };
    case "failed":
      return {
        ...common,
        enabled: true,
        state: "failed",
        hostname: "dmanager",
        error: "auth key rejected: example error",
      };
    case "disabled":
      return { ...common, enabled: false, state: "disabled" };
  }
}

export const SETTINGS = {
  gotifyUrl: "https://gotify.example.com",
  gotifyToken: "mock-token",
  auditRetentionDays: 90,
};

export const REGISTRIES = [
  {
    host: "registry-1.docker.io",
    username: "",
    isConfigured: false,
    isHealthy: true,
    errorMessage: "",
  },
  {
    host: "registry.example.com",
    username: "ci-user",
    isConfigured: true,
    isHealthy: true,
    errorMessage: "",
  },
  {
    host: "ghcr.io",
    username: "ci-user",
    isConfigured: true,
    isHealthy: false,
    errorMessage: "401 unauthorized (example)",
  },
];

export const IMAGES = [
  {
    id: "sha256:img1",
    repoTags: ["example.com/team/app-1:1.4.2"],
    createdUnix: BigInt(Math.floor(ago(5 * 24 * 60 * MINUTE).getTime() / 1000)),
    sizeBytes: 182_000_000n,
    containersCount: 1n,
  },
  {
    id: "sha256:img2",
    repoTags: ["postgres:17"],
    createdUnix: BigInt(Math.floor(ago(12 * 24 * 60 * MINUTE).getTime() / 1000)),
    sizeBytes: 435_000_000n,
    containersCount: 1n,
  },
  {
    id: "sha256:img3",
    repoTags: [],
    createdUnix: BigInt(Math.floor(ago(40 * 24 * 60 * MINUTE).getTime() / 1000)),
    sizeBytes: 98_000_000n,
    containersCount: 0n,
  },
];

export const VOLUMES: {
  name: string;
  driver: string;
  mountpoint: string;
  createdAt: ReturnType<typeof ts>;
  labels: Record<string, string>;
}[] = [
  {
    name: "app-1-data",
    driver: "local",
    mountpoint: "/var/lib/docker/volumes/app-1-data/_data",
    createdAt: ts(30 * 24 * 60 * MINUTE),
    labels: {},
  },
  {
    name: "db-1-data",
    driver: "local",
    mountpoint: "/var/lib/docker/volumes/db-1-data/_data",
    createdAt: ts(60 * 24 * 60 * MINUTE),
    labels: { "com.example.stack": "billing" },
  },
  {
    name: "unused-volume",
    driver: "local",
    mountpoint: "/var/lib/docker/volumes/unused-volume/_data",
    createdAt: ts(90 * 24 * 60 * MINUTE),
    labels: {},
  },
];

export const VOLUME_USAGE = [
  { name: "app-1-data", sizeBytes: 52_000_000n, refCount: 1n },
  { name: "db-1-data", sizeBytes: 1_200_000_000n, refCount: 1n },
  { name: "unused-volume", sizeBytes: 4_000_000n, refCount: 0n },
];

export const NETWORKS = [
  {
    id: "net1",
    name: "bridge",
    driver: "bridge",
    scope: "local",
    internal: false,
    createdAt: ts(100 * 24 * 60 * MINUTE),
    containersCount: 2n,
    predefined: true,
  },
  {
    id: "net2",
    name: "app-net",
    driver: "bridge",
    scope: "local",
    internal: false,
    createdAt: ts(30 * 24 * 60 * MINUTE),
    containersCount: 3n,
    predefined: false,
  },
  {
    id: "net3",
    name: "unused-net",
    driver: "bridge",
    scope: "local",
    internal: true,
    createdAt: ts(10 * 24 * 60 * MINUTE),
    containersCount: 0n,
    predefined: false,
  },
];

export const BUILD_CACHE = [
  {
    id: "bc1",
    type: "regular",
    description: "RUN go build ./...",
    sizeBytes: 220_000_000n,
    inUse: false,
    shared: false,
    usageCount: 3n,
    createdAt: ts(7 * 24 * 60 * MINUTE),
    lastUsedAt: ts(2 * 24 * 60 * MINUTE),
  },
  {
    id: "bc2",
    type: "source.local",
    description: "local context",
    sizeBytes: 12_000_000n,
    inUse: true,
    shared: true,
    usageCount: 1n,
    createdAt: ts(60 * MINUTE),
  },
];

export function auditFixtures() {
  const rows = [
    ["admin", "admin", 1, "container.upgrade", "container", "a3", 1, "upgraded db-1"],
    [
      "admin",
      "admin",
      1,
      "container.upgrade",
      "container",
      "a6",
      2,
      "container stack-web-1 is managed externally (compose-stack) and cannot be upgraded or auto-updated by dmanager",
    ],
    ["viewer-1", "viewer", 1, "image.delete", "image", "sha256:img3", 3, "permission denied"],
    ["system", "", 2, "container.auto_update", "container", "a2", 1, "auto-updated app-2-worker"],
    ["admin", "admin", 1, "network.prune", "network", "", 1, "removed 1 network: unused-net"],
    ["admin", "admin", 1, "settings.update", "settings", "", 1, "updated notification settings"],
  ] as const;
  return rows.map((r, i) => ({
    id: BigInt(rows.length - i),
    createdAt: ts((i + 1) * 37 * MINUTE),
    actor: r[0],
    actorRole: r[1],
    source: r[2],
    action: r[3],
    resourceType: r[4],
    resourceId: r[5],
    outcome: r[6],
    detail: r[7],
  }));
}

export function authEventFixtures() {
  return [
    ["login.success", "admin", "from 192.0.2.10"],
    ["login.failure", "admin", "bad password"],
    ["passkey.registered", "admin", "Laptop"],
  ].map(([eventType, username, detail], i) => ({
    id: BigInt(i + 1),
    userId: 1n,
    username,
    eventType,
    detail,
    createdAt: ts((i + 1) * 90 * MINUTE),
  }));
}

export function sessionFixtures() {
  return [
    {
      sessionId: "s1",
      userAgent: "Mozilla/5.0 (X11; Linux x86_64)",
      deviceLabel: "Browser on Linux",
      createdAt: ts(60 * MINUTE),
      lastSeenAt: ts(MINUTE),
      expiresAt: ts(-24 * 60 * MINUTE),
      absoluteExpiresAt: ts(-30 * 24 * 60 * MINUTE),
      isCurrent: true,
    },
    {
      sessionId: "s2",
      userAgent: "Mozilla/5.0 (iPhone)",
      deviceLabel: "Browser on iOS",
      createdAt: ts(3 * 24 * 60 * MINUTE),
      lastSeenAt: ts(5 * 60 * MINUTE),
      expiresAt: ts(-24 * 60 * MINUTE),
      absoluteExpiresAt: ts(-30 * 24 * 60 * MINUTE),
      isCurrent: false,
    },
  ];
}

export function passkeyFixtures() {
  return [
    {
      id: "pk1",
      name: "Laptop",
      aaguid: "",
      friendlyDeviceName: "Example Authenticator",
      backupEligible: true,
      backupState: true,
      signCount: 12,
      cloneWarning: false,
      createdAt: ts(20 * 24 * 60 * MINUTE),
      lastUsedAt: ts(60 * MINUTE),
    },
  ];
}

export const SYSTEM_LOGS = [
  { level: "INFO", message: "server listening on :9283", component: "server", metadata: "" },
  {
    level: "WARN",
    message: "registry ghcr.io unhealthy",
    component: "registry",
    metadata: '{"host":"ghcr.io"}',
  },
  {
    level: "ERROR",
    message: "failed to check updates for cache-1",
    component: "scheduler",
    metadata: "",
  },
].map((e, i) => ({ ...e, timestamp: agoIso((i + 1) * 5 * MINUTE) }));
