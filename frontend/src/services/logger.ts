import Dexie, { type Table } from "dexie";

export interface DBClientLogEntry {
  id?: number;
  level: "DEBUG" | "INFO" | "WARN" | "ERROR";
  message: string;
  timestamp: string; // RFC3339
  component: string;
  metadata: string; // stringified JSON
}

class ClientLogDatabase extends Dexie {
  logs!: Table<DBClientLogEntry>;

  constructor() {
    super("ClientLogDatabase");
    this.version(1).stores({
      logs: "++id, level, timestamp, component",
    });
  }
}

export const logDb = new ClientLogDatabase();

const originalWarn = console.warn;
const originalError = console.error;

let isIntercepting = false;

export async function addLogEntry(
  level: "DEBUG" | "INFO" | "WARN" | "ERROR",
  message: string,
  component: string,
  metadataObj: Record<string, unknown> = {},
) {
  try {
    const timestamp = new Date().toISOString();
    const metadata = JSON.stringify(metadataObj);
    await logDb.logs.add({
      level,
      message,
      timestamp,
      component,
      metadata,
    });
  } catch (err) {
    originalError.call(console, "Failed to save log to Dexie:", err);
  }
}

export function initLogger() {
  if (typeof window === "undefined") return;

  // Capture unhandled javascript errors
  window.addEventListener("error", (event) => {
    if (isIntercepting) return;
    isIntercepting = true;
    try {
      const message = event.message || "Unknown error";
      const metadata = {
        filename: event.filename,
        lineno: event.lineno,
        colno: event.colno,
        stack: event.error?.stack,
      };
      addLogEntry("ERROR", message, "WindowOnError", metadata);
    } finally {
      isIntercepting = false;
    }
  });

  // Capture unhandled promise rejections
  window.addEventListener("unhandledrejection", (event) => {
    if (isIntercepting) return;
    isIntercepting = true;
    try {
      const message = event.reason?.message || String(event.reason);
      const metadata = {
        stack: event.reason?.stack,
      };
      addLogEntry("ERROR", `Unhandled Rejection: ${message}`, "WindowOnUnhandledRejection", metadata);
    } finally {
      isIntercepting = false;
    }
  });

  // Wrap console.warn
  console.warn = function (...args) {
    originalWarn.apply(console, args);
    if (isIntercepting) return;
    isIntercepting = true;
    try {
      const message = args.map((arg) => (typeof arg === "object" ? JSON.stringify(arg) : String(arg))).join(" ");
      addLogEntry("WARN", message, "Console");
    } finally {
      isIntercepting = false;
    }
  };

  // Wrap console.error
  console.error = function (...args) {
    originalError.apply(console, args);
    if (isIntercepting) return;
    isIntercepting = true;
    try {
      const message = args.map((arg) => (typeof arg === "object" ? JSON.stringify(arg) : String(arg))).join(" ");
      const metadata = {
        stack: new Error().stack,
      };
      addLogEntry("ERROR", message, "Console", metadata);
    } finally {
      isIntercepting = false;
    }
  };

  // Capture user click events as action logs
  window.addEventListener(
    "click",
    (event) => {
      const target = event.target as HTMLElement | null;
      if (!target) return;

      const button = target.closest("button, a, input[type='submit'], input[type='button']");
      if (button) {
        const tag = button.tagName.toLowerCase();
        const text = button.textContent?.trim().slice(0, 50) || "";
        const id = button.id ? `#${button.id}` : "";
        const classes = button.className
          ? `.${Array.from(button.classList)
              .filter((c) => typeof c === "string" && c.trim() !== "")
              .join(".")}`
          : "";

        const identifier = `${tag}${id}${classes}`;
        addLogEntry("INFO", `User clicked: ${identifier} ("${text}")`, "UserAction", {
          tag,
          text,
          id: button.id || undefined,
          classes: button.className || undefined,
        });
      }
    },
    { capture: true },
  );
}

export function logUserAction(action: string, component: string, metadata: Record<string, unknown> = {}) {
  addLogEntry("INFO", action, component, metadata);
}
