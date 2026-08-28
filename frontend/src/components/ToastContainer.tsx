import { AlertCircle, AlertTriangle, CheckCircle2, Info, X } from "lucide-react";
import { useCallback, useEffect, useRef } from "react";

import { type ToastItem, useToast } from "../context/ToastContext";

interface ToastProps {
  toast: ToastItem;
  onClose: (id: string) => void;
}

function Toast({ toast, onClose }: ToastProps) {
  const { id, type, message, duration = 5000 } = toast;
  const timeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const startTimer = useCallback(() => {
    if (duration > 0) {
      timeoutRef.current = setTimeout(() => {
        onClose(id);
      }, duration);
    }
  }, [id, duration, onClose]);

  const clearTimer = useCallback(() => {
    if (timeoutRef.current) {
      clearTimeout(timeoutRef.current);
      timeoutRef.current = null;
    }
  }, []);

  // Start timer on mount, clear on unmount
  useEffect(() => {
    startTimer();
    return () => clearTimer();
  }, [startTimer, clearTimer]);

  let Icon = Info;
  if (type === "success") Icon = CheckCircle2;
  if (type === "error") Icon = AlertCircle;
  if (type === "warning") Icon = AlertTriangle;

  return (
    <div
      className={`toast-item toast-${type}`}
      role="status"
      onMouseEnter={clearTimer}
      onMouseLeave={startTimer}
    >
      <Icon className="toast-icon" size={18} />
      <div className="toast-content">{message}</div>
      <button
        type="button"
        className="toast-close-btn"
        onClick={() => onClose(id)}
        aria-label="Dismiss notification"
      >
        <X size={14} />
      </button>
    </div>
  );
}

export function ToastContainer() {
  const { toasts, removeToast } = useToast();

  if (toasts.length === 0) return null;

  return (
    <div className="toast-container" aria-live="assertive" aria-relevant="additions">
      {toasts.map((toast) => (
        <Toast key={toast.id} toast={toast} onClose={removeToast} />
      ))}
    </div>
  );
}
