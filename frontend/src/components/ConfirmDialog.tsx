import { Loader2 } from "lucide-react";

import { Dialog } from "./Dialog";

export interface ConfirmDialogProps {
  open: boolean;
  /** Cancel / dismiss (button, Esc, backdrop — suppressed while busy). */
  onClose: () => void;
  /** Confirm button only. */
  onConfirm: () => void;
  title: string;
  /** State the consequence, not the request (design.md §11.4). */
  message: string;
  /** Always a verb ("Delete", "Revoke"); default "Confirm". */
  confirmLabel?: string;
  cancelLabel?: string;
  /** danger → confirm button uses error tokens; default "default". */
  variant?: "default" | "danger";
  /** In-flight mutation: spinner on confirm, both buttons disabled, dismissal locked. */
  busy?: boolean;
}

/**
 * Confirmation specialization of Dialog (design.md §11.4). Safe by default:
 * danger dialogs focus the cancel action on open, and `busy` locks the whole
 * dialog — an in-flight RPC cannot be aborted mid-dialog (the result arrives
 * via toasts and the caller's own close handling).
 */
export function ConfirmDialog({
  open,
  onClose,
  onConfirm,
  title,
  message,
  confirmLabel = "Confirm",
  cancelLabel = "Cancel",
  variant = "default",
  busy = false,
}: ConfirmDialogProps) {
  const requestClose = () => {
    if (!busy) {
      onClose();
    }
  };

  return (
    <Dialog
      open={open}
      onClose={requestClose}
      title={title}
      description={message}
      initialFocus={variant === "danger" ? "cancel" : "confirm"}
      footer={
        <>
          <button
            type="button"
            className="dialog-cancel-btn"
            data-dialog-focus="cancel"
            onClick={requestClose}
            disabled={busy}
          >
            {cancelLabel}
          </button>
          <button
            type="button"
            className={`dialog-confirm-btn${variant === "danger" ? " danger" : ""}`}
            data-dialog-focus="confirm"
            onClick={onConfirm}
            disabled={busy}
          >
            {busy ? <Loader2 size={16} className="spinner" /> : null}
            {confirmLabel}
          </button>
        </>
      }
    />
  );
}
