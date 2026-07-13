import { act, fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { ToastProvider, useToast } from "../context/ToastContext";
import { ToastContainer } from "./ToastContainer";

function TestComponent() {
  const { success, error, info, warning } = useToast();
  return (
    <div>
      <button type="button" onClick={() => success("Success message")} data-testid="success-btn">
        Success
      </button>
      <button type="button" onClick={() => error("Error message")} data-testid="error-btn">
        Error
      </button>
      <button type="button" onClick={() => info("Info message")} data-testid="info-btn">
        Info
      </button>
      <button type="button" onClick={() => warning("Warning message")} data-testid="warning-btn">
        Warning
      </button>
      <button type="button" onClick={() => success("Short message", 1000)} data-testid="short-btn">
        Short
      </button>
    </div>
  );
}

describe("Toast Notification System", () => {
  it("renders toasts when triggered and removes them on close click", () => {
    render(
      <ToastProvider>
        <TestComponent />
        <ToastContainer />
      </ToastProvider>,
    );

    // No toast initially
    expect(screen.queryByText("Success message")).not.toBeInTheDocument();

    // Trigger success toast
    fireEvent.click(screen.getByTestId("success-btn"));
    expect(screen.getByText("Success message")).toBeInTheDocument();

    // Trigger error toast
    fireEvent.click(screen.getByTestId("error-btn"));
    expect(screen.getByText("Error message")).toBeInTheDocument();

    // Close the success toast
    const closeButtons = screen.getAllByRole("button", {
      name: /dismiss notification/i,
    });
    fireEvent.click(closeButtons[0]);

    // Success toast is removed, error toast remains
    expect(screen.queryByText("Success message")).not.toBeInTheDocument();
    expect(screen.getByText("Error message")).toBeInTheDocument();
  });

  it("supports all toast types with correct classes", () => {
    render(
      <ToastProvider>
        <TestComponent />
        <ToastContainer />
      </ToastProvider>,
    );

    fireEvent.click(screen.getByTestId("success-btn"));
    fireEvent.click(screen.getByTestId("error-btn"));
    fireEvent.click(screen.getByTestId("info-btn"));
    fireEvent.click(screen.getByTestId("warning-btn"));

    expect(screen.getByText("Success message").closest(".toast-item")).toHaveClass("toast-success");
    expect(screen.getByText("Error message").closest(".toast-item")).toHaveClass("toast-error");
    expect(screen.getByText("Info message").closest(".toast-item")).toHaveClass("toast-info");
    expect(screen.getByText("Warning message").closest(".toast-item")).toHaveClass("toast-warning");
  });

  it("automatically dismisses toast after duration", () => {
    vi.useFakeTimers();

    render(
      <ToastProvider>
        <TestComponent />
        <ToastContainer />
      </ToastProvider>,
    );

    // Trigger short toast (1000ms duration)
    fireEvent.click(screen.getByTestId("short-btn"));
    expect(screen.getByText("Short message")).toBeInTheDocument();

    // Fast-forward time by 1000ms
    act(() => {
      vi.advanceTimersByTime(1000);
    });

    expect(screen.queryByText("Short message")).not.toBeInTheDocument();

    vi.useRealTimers();
  });

  it("pauses dismissal on hover and resumes on mouse leave", () => {
    vi.useFakeTimers();

    render(
      <ToastProvider>
        <TestComponent />
        <ToastContainer />
      </ToastProvider>,
    );

    // Trigger short toast (1000ms duration)
    fireEvent.click(screen.getByTestId("short-btn"));
    const toastItem = screen.getByText("Short message").closest(".toast-item")!;
    expect(toastItem).toBeInTheDocument();

    // Hover over the toast
    fireEvent.mouseEnter(toastItem);

    // Fast-forward time by 1000ms (should NOT dismiss)
    act(() => {
      vi.advanceTimersByTime(1000);
    });
    expect(screen.getByText("Short message")).toBeInTheDocument();

    // Mouse leaves the toast
    fireEvent.mouseLeave(toastItem);

    // Fast-forward time by 1000ms (should dismiss now)
    act(() => {
      vi.advanceTimersByTime(1000);
    });
    expect(screen.queryByText("Short message")).not.toBeInTheDocument();

    vi.useRealTimers();
  });
});
