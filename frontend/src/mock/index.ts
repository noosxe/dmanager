import type { Interceptor } from "@connectrpc/connect";

import { SCENARIOS, TAILSCALE_VARIANTS, type Scenario, type TailscaleVariant } from "./fixtures";
import { createMockTransport } from "./handlers";

export { createMockTransport } from "./handlers";

// A query parameter picks the variant (`?scenario=login&tailscale=starting`)
// and is remembered for the tab, so in-app navigation keeps it.
function param<T extends string>(name: string, allowed: readonly T[], fallback: T): T {
  const fromUrl = new URLSearchParams(window.location.search).get(name);
  if (fromUrl && (allowed as readonly string[]).includes(fromUrl)) {
    sessionStorage.setItem(`mock:${name}`, fromUrl);
    return fromUrl as T;
  }
  const stored = sessionStorage.getItem(`mock:${name}`);
  return stored && (allowed as readonly string[]).includes(stored) ? (stored as T) : fallback;
}

/** Transport used by `client.ts` in mock mode (`vite --mode mock`). */
export function createBrowserMockTransport(interceptors: Interceptor[]) {
  return createMockTransport({
    scenario: param<Scenario>("scenario", SCENARIOS, "admin"),
    tailscale: param<TailscaleVariant>("tailscale", TAILSCALE_VARIANTS, "running"),
    interceptors,
  });
}
