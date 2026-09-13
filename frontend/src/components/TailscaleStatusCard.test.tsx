import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import type { TailscaleStatusState } from "../hooks/useTailscaleStatus";
import { keyExpiryDays, TailscaleStatusCard } from "./TailscaleStatusCard";

const base: TailscaleStatusState = {
  loaded: true,
  enabled: true,
  state: "running",
  backendState: "Running",
  hostname: "dmanager",
  dnsName: "dmanager.tail1234.ts.net",
  ips: ["100.64.0.1"],
  port: 80,
  httpsEnabled: true,
  certDomains: ["dmanager.tail1234.ts.net"],
  error: "",
};

describe("TailscaleStatusCard", () => {
  afterEach(cleanup);

  it("renders nothing when the feature is disabled", () => {
    const { container } = render(<TailscaleStatusCard status={{ ...base, enabled: false }} />);
    expect(container.querySelector('[data-testid="tailscale-status-card"]')).toBeNull();
  });

  it("shows identity, access URLs and the running state", () => {
    render(<TailscaleStatusCard status={base} />);
    const card = screen.getByTestId("tailscale-status-card");
    expect(card.textContent).toContain("Tailscale node Online");
    expect(card.textContent).toContain("dmanager.tail1234.ts.net");
    expect(card.textContent).toContain("100.64.0.1");
    expect(card.textContent).toContain("https://dmanager.tail1234.ts.net");
  });

  it("shows the plain http URL when https is off", () => {
    render(<TailscaleStatusCard status={{ ...base, httpsEnabled: false }} />);
    expect(screen.getByTestId("tailscale-status-card").textContent).toContain(
      "http://dmanager.tail1234.ts.net:80",
    );
  });

  it("shows the failure detail in degraded mode", () => {
    render(
      <TailscaleStatusCard
        status={{ ...base, state: "failed", error: "tailscale node failed to connect: bad key" }}
      />,
    );
    const card = screen.getByTestId("tailscale-status-card");
    expect(card.textContent).toContain("Failed");
    expect(card.textContent).toContain("bad key");
  });

  it("warns when the node key expires within a week", () => {
    render(
      <TailscaleStatusCard
        status={{ ...base, keyExpiry: new Date(Date.now() + 2 * 86_400_000 + 3_600_000) }}
      />,
    );
    expect(screen.getByTestId("tailscale-status-card").textContent).toContain(
      "Node key expires in 2 days",
    );
  });

  it("warns when the node key has expired", () => {
    render(
      <TailscaleStatusCard status={{ ...base, keyExpiry: new Date(Date.now() - 86_400_000) }} />,
    );
    expect(screen.getByTestId("tailscale-status-card").textContent).toContain("Node key expired");
  });

  it("stays quiet when the key expiry is far away or unset", () => {
    const far = render(
      <TailscaleStatusCard
        status={{ ...base, keyExpiry: new Date(Date.now() + 90 * 86_400_000) }}
      />,
    );
    expect(far.container.textContent).not.toContain("Node key");
    cleanup();
    const none = render(<TailscaleStatusCard status={base} />);
    expect(none.container.textContent).not.toContain("Node key");
  });
});

describe("keyExpiryDays", () => {
  it("returns null for unset expiry", () => {
    expect(keyExpiryDays(undefined)).toBeNull();
  });

  it("floors to whole days and goes negative after expiry", () => {
    const d = keyExpiryDays(new Date(Date.now() + 3 * 86_400_000 + 3_600_000));
    expect(d).toBe(3);
    const past = keyExpiryDays(new Date(Date.now() - 5 * 86_400_000));
    expect(past).toBe(-5);
  });
});
