import type React from "react";
import { useEffect, useId, useRef } from "react";
import { createPortal } from "react-dom";

const FOCUSABLE_SELECTOR =
  'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

export interface DialogProps {
  open: boolean;
  /** Every dismiss path (Esc, backdrop) funnels here. */
  onClose: () => void;
  title: string;
  /** Optional secondary line; wired to aria-describedby. */
  description?: string;
  children?: React.ReactNode;
  /** Action buttons row; mark buttons with data-dialog-focus (see initialFocus). */
  footer?: React.ReactNode;
  /** Which footer button receives focus on open — "cancel" is the safe default. */
  initialFocus?: "confirm" | "cancel";
}

/**
 * The app's modal primitive (design.md §11.3): a self-contained portal with a
 * focus trap, Esc/backdrop dismissal, aria wiring, body scroll lock, and focus
 * restoration to the opener. Declarative by design — callers own `open` state;
 * there is deliberately no imperative dialog registry (the rationale lives in
 * §11.2 next to the toast system's imperative API).
 *
 * Specializations mark their footer buttons with `data-dialog-focus="confirm"`
 * / `data-dialog-focus="cancel"` so `initialFocus` can find them.
 */
export function Dialog({
  open,
  onClose,
  title,
  description,
  children,
  footer,
  initialFocus = "cancel",
}: DialogProps) {
  const titleId = useId();
  const descriptionId = useId();
  const cardRef = useRef<HTMLDivElement>(null);
  const restoreFocusRef = useRef<HTMLElement | null>(null);

  // Esc dismissal + Tab focus trap. Capture-phase document listener: works no
  // matter where focus sits, including right after open before the initial
  // focus effect has run.
  useEffect(() => {
    if (!open) {
      return;
    }

    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        onClose();
        return;
      }
      if (e.key !== "Tab") {
        return;
      }

      const card = cardRef.current;
      if (!card) {
        return;
      }
      const focusables = Array.from(card.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR));
      if (focusables.length === 0) {
        e.preventDefault();
        card.focus();
        return;
      }

      const first = focusables[0];
      const last = focusables[focusables.length - 1];
      const active = document.activeElement;
      if (e.shiftKey) {
        if (active === first || !card.contains(active)) {
          e.preventDefault();
          last.focus();
        }
      } else if (active === last || !card.contains(active)) {
        e.preventDefault();
        first.focus();
      }
    };

    document.addEventListener("keydown", onKeyDown, true);
    return () => document.removeEventListener("keydown", onKeyDown, true);
  }, [open, onClose]);

  // On open: remember the opener, lock background scroll, move focus inside.
  // On close/unmount: unlock and restore focus to the opener (design.md §11.3).
  useEffect(() => {
    if (!open) {
      return;
    }

    restoreFocusRef.current = document.activeElement as HTMLElement | null;
    document.documentElement.classList.add("dialog-scroll-lock");

    const card = cardRef.current;
    if (card) {
      const target =
        initialFocus === "confirm"
          ? card.querySelector<HTMLElement>('[data-dialog-focus="confirm"]')
          : card.querySelector<HTMLElement>('[data-dialog-focus="cancel"]');
      (target ?? card.querySelector<HTMLElement>(FOCUSABLE_SELECTOR) ?? card).focus();
    }

    return () => {
      document.documentElement.classList.remove("dialog-scroll-lock");
      restoreFocusRef.current?.focus();
    };
  }, [open, initialFocus]);

  if (!open) {
    return null;
  }

  return createPortal(
    <div
      className="dialog-overlay"
      onMouseDown={(e) => {
        // Dismiss only for clicks on the backdrop itself, never the card.
        if (e.target === e.currentTarget) {
          onClose();
        }
      }}
    >
      <div
        ref={cardRef}
        className="dialog-card"
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        aria-describedby={description ? descriptionId : undefined}
        tabIndex={-1}
      >
        <h2 className="dialog-title" id={titleId}>
          {title}
        </h2>
        {description ? (
          <p className="dialog-message" id={descriptionId}>
            {description}
          </p>
        ) : null}
        {children}
        {footer ? <div className="dialog-footer">{footer}</div> : null}
      </div>
    </div>,
    document.body,
  );
}
