import { AlertCircle, AlertTriangle, CheckCircle2, Info, X } from "lucide-react";
import { useToast } from "../context/ToastContext";

export function ToastContainer() {
  const { toasts, removeToast } = useToast();

  if (toasts.length === 0) return null;

  return (
    <div className="toast-container" aria-live="assertive" aria-relevant="additions">
      {toasts.map((toast) => {
        let Icon = Info;
        if (toast.type === "success") Icon = CheckCircle2;
        if (toast.type === "error") Icon = AlertCircle;
        if (toast.type === "warning") Icon = AlertTriangle;

        return (
          <div key={toast.id} className={`toast-item toast-${toast.type}`} role="status">
            <Icon className="toast-icon" size={18} />
            <div className="toast-content">{toast.message}</div>
            <button
              type="button"
              className="toast-close-btn"
              onClick={() => removeToast(toast.id)}
              aria-label="Dismiss notification"
            >
              <X size={14} />
            </button>
          </div>
        );
      })}
    </div>
  );
}
