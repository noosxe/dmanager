import { afterEach, describe, expect, it, vi } from "vitest";

import { getBaseUrl } from "./client";

describe("getBaseUrl", () => {
  afterEach(() => {
    vi.unstubAllEnvs();
    vi.unstubAllGlobals();
  });

  it("uses relative URLs in production builds regardless of the page port", () => {
    vi.stubEnv("DEV", false);
    vi.stubGlobal("location", { port: "3000" });
    expect(getBaseUrl()).toBe("");
    vi.stubGlobal("location", { port: "5173" });
    expect(getBaseUrl()).toBe("");
  });

  it("points at the backend on 9283 in dev mode", () => {
    vi.stubEnv("DEV", true);
    expect(getBaseUrl()).toBe("http://localhost:9283");
  });
});
