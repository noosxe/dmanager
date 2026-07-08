import { FitAddon } from "@xterm/addon-fit";
import { Terminal as TerminalIcon, Trash2, X } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { Terminal } from "xterm";
import { containerClient } from "../client";

import "xterm/css/xterm.css";

interface LogsDrawerProps {
  containerId: string | null;
  containerName: string | null;
  onClose: () => void;
}

export function LogsDrawer({ containerId, containerName, onClose }: LogsDrawerProps) {
  const [tailLines, setTailLines] = useState<number>(100);
  const [follow, setFollow] = useState<boolean>(true);
  const [status, setStatus] = useState<"connecting" | "streaming" | "disconnected" | "error">(
    "disconnected",
  );

  const terminalRef = useRef<HTMLDivElement>(null);
  const xtermRef = useRef<Terminal | null>(null);
  const fitAddonRef = useRef<FitAddon | null>(null);
  const resizeObserverRef = useRef<ResizeObserver | null>(null);

  const isOpen = !!containerId;

  // Initialize xterm
  useEffect(() => {
    if (!isOpen || !terminalRef.current) return;

    const term = new Terminal({
      cursorBlink: true,
      theme: {
        background: "#0d0e12",
        foreground: "#e4e4e7",
        cursor: "#aa3bff",
        selectionBackground: "rgba(170, 59, 255, 0.3)",
        black: "#18181b",
        red: "#f87171",
        green: "#34d399",
        yellow: "#fbbf24",
        blue: "#60a5fa",
        magenta: "#c084fc",
        cyan: "#2dd4bf",
        white: "#f4f4f5",
      },
      fontFamily: "var(--font-mono)",
      fontSize: 13,
      lineHeight: 1.4,
      scrollback: 5000,
      convertEol: true,
    });

    const fitAddon = new FitAddon();
    term.loadAddon(fitAddon);
    term.open(terminalRef.current);
    fitAddon.fit();

    xtermRef.current = term;
    fitAddonRef.current = fitAddon;

    // Use ResizeObserver to automatically fit xterm to the parent's size
    const resizeObserver = new ResizeObserver(() => {
      try {
        fitAddon.fit();
      } catch (e) {
        // Fit Addon can throw if element is not visible or has no width/height
        console.warn("xterm fit failed:", e);
      }
    });

    resizeObserver.observe(terminalRef.current);
    resizeObserverRef.current = resizeObserver;

    return () => {
      resizeObserver.disconnect();
      term.dispose();
      xtermRef.current = null;
      fitAddonRef.current = null;
      resizeObserverRef.current = null;
    };
  }, [isOpen]);

  // Subscribe to StreamLogs / GetContainerLogs
  useEffect(() => {
    if (!isOpen || !containerId || !xtermRef.current) return;

    const term = xtermRef.current;
    const abortController = new AbortController();
    let isSubscribed = true;

    const streamLogs = async () => {
      setStatus("connecting");
      term.clear();
      term.write("\x1b[90mConnecting to logs stream...\x1b[0m\r\n");

      try {
        const stream = await containerClient.getContainerLogs(
          {
            id: containerId,
            tailLines: tailLines,
            follow: follow,
          },
          { signal: abortController.signal },
        );

        if (!isSubscribed) return;
        setStatus("streaming");
        term.write("\x1b[32mConnected. Streaming container logs:\x1b[0m\r\n\r\n");

        for await (const chunk of stream) {
          if (!isSubscribed) break;
          term.write(`${chunk.logLine}\r\n`);
        }

        if (isSubscribed) {
          setStatus("disconnected");
          term.write("\r\n\x1b[90mStream closed by server.\x1b[0m\r\n");
        }
      } catch (err: unknown) {
        if (abortController.signal.aborted) {
          if (isSubscribed) {
            setStatus("disconnected");
            term.write("\r\n\x1b[90mStream paused.\x1b[0m\r\n");
          }
          return;
        }

        console.error("Log stream error:", err);
        const msg = err instanceof Error ? err.message : String(err);
        if (isSubscribed) {
          setStatus("error");
          term.write(`\r\n\x1b[31mError: ${msg}\x1b[0m\r\n`);
        }
      }
    };

    streamLogs();

    return () => {
      isSubscribed = false;
      abortController.abort();
    };
  }, [isOpen, containerId, tailLines, follow]);

  // Clear terminal action
  const handleClear = () => {
    if (xtermRef.current) {
      xtermRef.current.clear();
    }
  };

  return (
    <>
      <button
        type="button"
        className={`logs-drawer-overlay ${isOpen ? "open" : ""}`}
        onClick={onClose}
        aria-label="Close logs drawer"
      />
      <div className={`logs-drawer-panel ${isOpen ? "open" : ""}`}>
        {/* Header section */}
        <div className="logs-drawer-header">
          <div className="logs-drawer-title-group">
            <TerminalIcon className="logs-drawer-icon" size={20} />
            <div>
              <h3>Logs Console</h3>
              <div className="logs-drawer-subtitle">{containerName || containerId || ""}</div>
            </div>
          </div>
          <button
            type="button"
            className="logs-drawer-close-btn"
            onClick={onClose}
            aria-label="Close logs drawer"
          >
            <X size={18} />
          </button>
        </div>

        {/* Control bar */}
        <div className="logs-drawer-controls">
          <div className="logs-drawer-filters">
            <div className="logs-control-group">
              <span>Lines:</span>
              <select
                className="logs-select"
                value={tailLines}
                onChange={(e) => setTailLines(Number(e.target.value))}
              >
                <option value={50}>50</option>
                <option value={100}>100</option>
                <option value={250}>250</option>
                <option value={500}>500</option>
                <option value={1000}>1000</option>
              </select>
            </div>

            <div className="logs-control-group">
              <label className="logs-checkbox-label">
                <input
                  type="checkbox"
                  className="logs-checkbox"
                  checked={follow}
                  onChange={(e) => setFollow(e.target.checked)}
                />
                <span>Follow Stream</span>
              </label>
            </div>
          </div>

          <div className="logs-action-buttons">
            <span className={`logs-status-badge ${status}`}>
              <span
                style={{
                  width: "6px",
                  height: "6px",
                  borderRadius: "50%",
                  background:
                    status === "streaming"
                      ? "#10b981"
                      : status === "connecting"
                        ? "#f59e0b"
                        : status === "error"
                          ? "#ef4444"
                          : "#6b7280",
                  display: "inline-block",
                }}
              />
              <span>{status}</span>
            </span>

            <button
              type="button"
              className="logs-btn"
              onClick={handleClear}
              title="Clear terminal buffer"
            >
              <Trash2 size={13} />
              <span>Clear</span>
            </button>
          </div>
        </div>

        {/* Terminal screen container */}
        <div className="terminal-container">
          <div className="xterm-viewport-wrapper">
            <div ref={terminalRef} style={{ width: "100%", height: "100%" }} />
          </div>
        </div>
      </div>
    </>
  );
}
