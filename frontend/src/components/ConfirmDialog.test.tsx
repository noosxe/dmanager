import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { ConfirmDialog } from "./ConfirmDialog";

function renderConfirm(props: Partial<Parameters<typeof ConfirmDialog>[0]> = {}) {
  const onClose = vi.fn();
  const onConfirm = vi.fn();
  render(
    <ConfirmDialog
      open
      onClose={onClose}
      onConfirm={onConfirm}
      title="Delete passkey?"
      message="This passkey will be removed. You will not be able to sign in with it afterwards."
      confirmLabel="Delete"
      variant="danger"
      {...props}
    />,
  );
  return { onClose, onConfirm };
}

describe("ConfirmDialog", () => {
  afterEach(() => {
    document.documentElement.className = "";
  });

  it("renders the title, consequence message, and both actions", () => {
    renderConfirm();

    const dialog = screen.getByRole("dialog");
    expect(dialog).toHaveAccessibleName("Delete passkey?");
    expect(dialog).toHaveAccessibleDescription(
      "This passkey will be removed. You will not be able to sign in with it afterwards.",
    );
    expect(screen.getByRole("button", { name: "Delete" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Cancel" })).toBeInTheDocument();
  });

  it("marks the confirm button as danger", () => {
    renderConfirm();
    expect(screen.getByRole("button", { name: "Delete" })).toHaveClass("danger");
  });

  it("focuses Cancel for danger variants — the safe action gets Enter", () => {
    renderConfirm();
    expect(document.activeElement).toBe(screen.getByRole("button", { name: "Cancel" }));
  });

  it("focuses Confirm for default variants", () => {
    renderConfirm({ variant: "default" });
    // confirmLabel stays "Delete" from the helper; default variant focuses it.
    expect(document.activeElement).toBe(screen.getByRole("button", { name: "Delete" }));
  });

  it("fires onConfirm from the confirm button only", () => {
    const { onConfirm, onClose } = renderConfirm();

    fireEvent.click(screen.getByRole("button", { name: "Delete" }));
    expect(onConfirm).toHaveBeenCalledTimes(1);
    expect(onClose).not.toHaveBeenCalled();
  });

  it("closes via the cancel button and via Escape", () => {
    const { onClose, onConfirm } = renderConfirm();

    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(onClose).toHaveBeenCalledTimes(1);

    fireEvent.keyDown(document, { key: "Escape" });
    expect(onClose).toHaveBeenCalledTimes(2);
    expect(onConfirm).not.toHaveBeenCalled();
  });

  it("locks the dialog while busy: both buttons disabled, no dismissal, spinner shown", () => {
    const { onClose } = renderConfirm({ busy: true });
    const confirm = screen.getByRole("button", { name: "Delete" });
    const cancel = screen.getByRole("button", { name: "Cancel" });

    expect(confirm).toBeDisabled();
    expect(cancel).toBeDisabled();
    expect(confirm.querySelector(".spinner")).not.toBeNull();

    // Esc and backdrop are suppressed while the mutation is in flight.
    fireEvent.keyDown(document, { key: "Escape" });
    fireEvent.mouseDown(document.querySelector(".dialog-overlay") as Element);
    expect(onClose).not.toHaveBeenCalled();
  });
});
