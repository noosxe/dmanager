import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { Dialog } from "./Dialog";

function renderDialog(props: Partial<Parameters<typeof Dialog>[0]> = {}) {
  const onClose = vi.fn();
  const utils = render(
    <Dialog
      open
      onClose={onClose}
      title="Test title"
      description="Test description"
      footer={
        <>
          <button type="button" data-dialog-focus="cancel">
            Cancel
          </button>
          <button type="button" data-dialog-focus="confirm">
            Confirm
          </button>
        </>
      }
      {...props}
    >
      <p>body content</p>
    </Dialog>,
  );
  return { onClose, ...utils };
}

describe("Dialog", () => {
  afterEach(() => {
    document.documentElement.className = "";
  });

  it("renders nothing when closed", () => {
    const { container } = renderDialog({ open: false });
    expect(container.innerHTML).toBe("");
    expect(document.querySelector(".dialog-overlay")).toBeNull();
    expect(document.documentElement).not.toHaveClass("dialog-scroll-lock");
  });

  it("renders a labelled modal dialog in a portal", () => {
    renderDialog();

    const dialog = screen.getByRole("dialog");
    expect(dialog).toHaveAttribute("aria-modal", "true");
    expect(dialog).toHaveAccessibleName("Test title");
    expect(dialog).toHaveAccessibleDescription("Test description");
    expect(screen.getByText("body content")).toBeInTheDocument();
    // Portaled to body, not inline.
    expect(document.querySelector(".dialog-overlay")?.parentElement).toBe(document.body);
  });

  it("omits aria-describedby when there is no description", () => {
    renderDialog({ description: undefined });
    expect(screen.getByRole("dialog")).not.toHaveAttribute("aria-describedby");
  });

  it("dismisses on Escape", () => {
    const { onClose } = renderDialog();
    fireEvent.keyDown(document, { key: "Escape" });
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("dismisses on a mousedown on the backdrop, not inside the card", () => {
    const { onClose } = renderDialog();

    fireEvent.mouseDown(screen.getByRole("dialog"));
    expect(onClose).not.toHaveBeenCalled();

    fireEvent.mouseDown(document.querySelector(".dialog-overlay") as Element);
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("locks background scroll while open and unlocks on close", () => {
    const { rerender } = renderDialog();
    expect(document.documentElement).toHaveClass("dialog-scroll-lock");

    rerender(
      <Dialog open={false} onClose={vi.fn()} title="Test title">
        <p>body content</p>
      </Dialog>,
    );
    expect(document.documentElement).not.toHaveClass("dialog-scroll-lock");
  });

  it("removes the scroll lock when unmounted while open", () => {
    const { unmount } = renderDialog();
    expect(document.documentElement).toHaveClass("dialog-scroll-lock");
    unmount();
    expect(document.documentElement).not.toHaveClass("dialog-scroll-lock");
  });

  it("focuses the cancel button by default — never pre-arms the destructive action", () => {
    renderDialog();
    expect(document.activeElement).toBe(screen.getByText("Cancel"));
  });

  it("focuses the confirm button when initialFocus is confirm", () => {
    renderDialog({ initialFocus: "confirm" });
    expect(document.activeElement).toBe(screen.getByText("Confirm"));
  });

  it("traps Tab focus at the card edges, wrapping in both directions", () => {
    renderDialog();
    const cancel = screen.getByText("Cancel");
    const confirm = screen.getByText("Confirm");

    // The trap only intercepts at the edges — interior moves are native Tab
    // behavior, which jsdom does not perform, so the tests start at an edge.
    // Forward Tab from the LAST focusable wraps to the first.
    confirm.focus();
    fireEvent.keyDown(document, { key: "Tab" });
    expect(document.activeElement).toBe(cancel);

    // Shift+Tab from the FIRST focusable wraps to the last.
    cancel.focus();
    fireEvent.keyDown(document, { key: "Tab", shiftKey: true });
    expect(document.activeElement).toBe(confirm);
  });

  it("does not intercept forward Tab from the first element (native order takes over)", () => {
    renderDialog();
    const cancel = screen.getByText("Cancel");
    const confirm = screen.getByText("Confirm");

    cancel.focus();
    fireEvent.keyDown(document, { key: "Tab" });
    // First → last is an interior move in DOM order; the trap must not force
    // a jump (jsdom leaves focus where it was instead of moving natively).
    expect(document.activeElement).toBe(cancel);
    expect(document.activeElement).not.toBe(confirm);
  });

  it("restores focus to the opener element on close", () => {
    const opener = document.createElement("button");
    opener.textContent = "opener";
    document.body.appendChild(opener);
    opener.focus();

    const { rerender } = renderDialog();
    expect(document.activeElement).not.toBe(opener);

    rerender(
      <Dialog open={false} onClose={vi.fn()} title="Test title">
        <p>body content</p>
      </Dialog>,
    );
    expect(document.activeElement).toBe(opener);
    opener.remove();
  });
});
