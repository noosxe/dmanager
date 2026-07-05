import { beforeEach, describe, expect, it, vi } from "vitest";

const { mockAdd, mockLimit, mockToArray, mockBulkDelete } = vi.hoisted(() => {
  return {
    mockAdd: vi.fn().mockResolvedValue(undefined),
    mockLimit: vi.fn(),
    mockToArray: vi.fn(),
    mockBulkDelete: vi.fn().mockResolvedValue(undefined),
  };
});

// Mock the dexie library with a dummy class
vi.mock("dexie", () => {
  return {
    default: class MockDexie {
      version() {
        return {
          stores: () => {},
        };
      }
    },
  };
});

import { addLogEntry, initLogger, logDb, logUserAction } from "./logger";

// Direct assignment to bypass any TypeScript class field initialization overrides
logDb.logs = {
  add: mockAdd,
  limit: mockLimit,
  toArray: mockToArray,
  bulkDelete: mockBulkDelete,
} as any;

describe("Client-Side Logger", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("should add log entry correctly via addLogEntry", async () => {
    await addLogEntry("INFO", "Test message", "TestComponent", { key: "value" });

    expect(mockAdd).toHaveBeenCalledTimes(1);
    const callArg = vi.mocked(mockAdd).mock.calls[0][0];
    expect(callArg.level).toBe("INFO");
    expect(callArg.message).toBe("Test message");
    expect(callArg.component).toBe("TestComponent");
    expect(JSON.parse(callArg.metadata)).toEqual({ key: "value" });
    expect(callArg.timestamp).toBeDefined();
  });

  it("should support logUserAction helper", async () => {
    await logUserAction("Action performed", "MyComponent", { user: "admin" });

    expect(mockAdd).toHaveBeenCalledTimes(1);
    const callArg = vi.mocked(mockAdd).mock.calls[0][0];
    expect(callArg.level).toBe("INFO");
    expect(callArg.message).toBe("Action performed");
    expect(callArg.component).toBe("MyComponent");
  });

  it("should intercept console.warn and console.error after initLogger", () => {
    const originalWarn = console.warn;
    const originalError = console.error;

    try {
      initLogger();

      console.warn("warning statement");
      expect(mockAdd).toHaveBeenCalledWith(
        expect.objectContaining({
          level: "WARN",
          message: "warning statement",
          component: "Console",
        }),
      );

      console.error("error statement");
      expect(mockAdd).toHaveBeenCalledWith(
        expect.objectContaining({
          level: "ERROR",
          message: "error statement",
          component: "Console",
        }),
      );
    } finally {
      console.warn = originalWarn;
      console.error = originalError;
    }
  });

  it("should intercept window errors and unhandled rejections", () => {
    initLogger();

    const errorEvent = new ErrorEvent("error", {
      message: "Uncaught test error",
      filename: "test.js",
      lineno: 42,
      colno: 10,
      error: new Error("Test error object"),
    });
    window.dispatchEvent(errorEvent);

    expect(mockAdd).toHaveBeenCalledWith(
      expect.objectContaining({
        level: "ERROR",
        message: "Uncaught test error",
        component: "WindowOnError",
      }),
    );

    const rejectedPromise = Promise.reject(new Error("Promise failed"));
    // Attach dummy catch handler so the runner doesn't fail on unhandled rejection
    rejectedPromise.catch(() => {});

    const promiseRejectionEvent = new PromiseRejectionEvent("unhandledrejection", {
      promise: rejectedPromise,
      reason: new Error("Promise failed"),
    });
    window.dispatchEvent(promiseRejectionEvent);

    expect(mockAdd).toHaveBeenCalledWith(
      expect.objectContaining({
        level: "ERROR",
        message: "Unhandled Rejection: Promise failed",
        component: "WindowOnUnhandledRejection",
      }),
    );
  });

  it("should capture button and link clicks as user actions", () => {
    initLogger();

    const btn = document.createElement("button");
    btn.id = "my-btn";
    btn.className = "btn-class primary";
    btn.textContent = "Click Me";
    document.body.appendChild(btn);

    btn.click();

    expect(mockAdd).toHaveBeenCalledWith(
      expect.objectContaining({
        level: "INFO",
        message: 'User clicked: button#my-btn.btn-class.primary ("Click Me")',
        component: "UserAction",
      }),
    );

    document.body.removeChild(btn);
  });
});
