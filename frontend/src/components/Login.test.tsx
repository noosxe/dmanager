import { Code, ConnectError } from "@connectrpc/connect";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { useAuth } from "../hooks/useAuth";
import { Login } from "./Login";

// Mock the useAuth hook
vi.mock("../hooks/useAuth", () => ({
  useAuth: vi.fn(),
}));

describe("Login Component", () => {
  const loginMock = vi.fn();

  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(useAuth).mockReturnValue({
      login: loginMock,
    } as unknown as ReturnType<typeof useAuth>);
  });

  it("renders username, password inputs and a submit button", () => {
    render(<Login />);
    expect(screen.getByLabelText(/username/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/password/i, { selector: "input" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /sign in/i })).toBeInTheDocument();
  });

  it("shows validation errors when fields are empty on submit", () => {
    render(<Login />);
    const submitBtn = screen.getByRole("button", { name: /sign in/i });
    fireEvent.click(submitBtn);

    expect(screen.getByText(/username is required/i)).toBeInTheDocument();
    expect(screen.getByText(/password is required/i)).toBeInTheDocument();
    expect(loginMock).not.toHaveBeenCalled();
  });

  it("shows validation errors for invalid inputs", () => {
    render(<Login />);
    const usernameInput = screen.getByLabelText(/username/i);
    const passwordInput = screen.getByLabelText(/password/i, { selector: "input" });
    const submitBtn = screen.getByRole("button", { name: /sign in/i });

    fireEvent.change(usernameInput, { target: { value: "ab" } });
    fireEvent.change(passwordInput, { target: { value: "12345" } });
    fireEvent.click(submitBtn);

    expect(screen.getByText(/username must be at least 3 characters/i)).toBeInTheDocument();
    expect(screen.getByText(/password must be at least 6 characters/i)).toBeInTheDocument();
    expect(loginMock).not.toHaveBeenCalled();
  });

  it("toggles password visibility when the show/hide button is clicked", () => {
    render(<Login />);
    const passwordInput = screen.getByLabelText(/password/i, {
      selector: "input",
    }) as HTMLInputElement;
    const toggleBtn = screen.getByRole("button", { name: /show password/i });

    expect(passwordInput.type).toBe("password");

    // Click toggle to show
    fireEvent.click(toggleBtn);
    expect(passwordInput.type).toBe("text");

    // Click toggle to hide
    fireEvent.click(toggleBtn);
    expect(passwordInput.type).toBe("password");
  });

  it("submits the form and calls login with valid inputs", async () => {
    loginMock.mockResolvedValueOnce(undefined);

    render(<Login />);
    const usernameInput = screen.getByLabelText(/username/i);
    const passwordInput = screen.getByLabelText(/password/i, { selector: "input" });
    const submitBtn = screen.getByRole("button", { name: /sign in/i });

    fireEvent.change(usernameInput, { target: { value: "admin" } });
    fireEvent.change(passwordInput, { target: { value: "password123" } });
    fireEvent.click(submitBtn);

    await waitFor(() => {
      expect(loginMock).toHaveBeenCalledWith("admin", "password123", false);
    });
  });

  it("submits the form with rememberMe true when checked", async () => {
    loginMock.mockResolvedValueOnce(undefined);

    render(<Login />);
    const usernameInput = screen.getByLabelText(/username/i);
    const passwordInput = screen.getByLabelText(/password/i, { selector: "input" });
    const rememberMeCheckbox = screen.getByLabelText(/remember me/i);
    const submitBtn = screen.getByRole("button", { name: /sign in/i });

    expect(rememberMeCheckbox).not.toBeChecked();
    fireEvent.click(rememberMeCheckbox);
    expect(rememberMeCheckbox).toBeChecked();

    fireEvent.change(usernameInput, { target: { value: "admin" } });
    fireEvent.change(passwordInput, { target: { value: "password123" } });
    fireEvent.click(submitBtn);

    await waitFor(() => {
      expect(loginMock).toHaveBeenCalledWith("admin", "password123", true);
    });
  });

  it("displays API error banner if login fails", async () => {
    const apiError = new Error("Invalid username or password");
    loginMock.mockRejectedValueOnce(apiError);

    render(<Login />);
    const usernameInput = screen.getByLabelText(/username/i);
    const passwordInput = screen.getByLabelText(/password/i, { selector: "input" });
    const submitBtn = screen.getByRole("button", { name: /sign in/i });

    fireEvent.change(usernameInput, { target: { value: "admin" } });
    fireEvent.change(passwordInput, { target: { value: "password123" } });
    fireEvent.click(submitBtn);

    await waitFor(() => {
      expect(screen.getByText("Invalid username or password")).toBeInTheDocument();
    });
  });

  it("displays rate limiting lockout error banner", async () => {
    const rateLimitError = new ConnectError(
      "too many failed login attempts, please try again in 60 seconds",
      Code.ResourceExhausted,
    );
    loginMock.mockRejectedValueOnce(rateLimitError);

    render(<Login />);
    const usernameInput = screen.getByLabelText(/username/i);
    const passwordInput = screen.getByLabelText(/password/i, { selector: "input" });
    const submitBtn = screen.getByRole("button", { name: /sign in/i });

    fireEvent.change(usernameInput, { target: { value: "admin" } });
    fireEvent.change(passwordInput, { target: { value: "password123" } });
    fireEvent.click(submitBtn);

    await waitFor(() => {
      expect(
        screen.getByText(/too many failed login attempts, please try again in 60 seconds/i),
      ).toBeInTheDocument();
    });
  });

  it("renders passkey button when passkeyLoginEnabled is true", () => {
    const loginWithPasskeyMock = vi.fn();
    vi.mocked(useAuth).mockReturnValue({
      login: loginMock,
      loginWithPasskey: loginWithPasskeyMock,
      passkeyLoginEnabled: true,
    } as unknown as ReturnType<typeof useAuth>);

    render(<Login />);
    expect(screen.getByRole("button", { name: /sign in with passkey/i })).toBeInTheDocument();
  });

  it("does not render passkey button when passkeyLoginEnabled is false", () => {
    const loginWithPasskeyMock = vi.fn();
    vi.mocked(useAuth).mockReturnValue({
      login: loginMock,
      loginWithPasskey: loginWithPasskeyMock,
      passkeyLoginEnabled: false,
    } as unknown as ReturnType<typeof useAuth>);

    render(<Login />);
    expect(screen.queryByRole("button", { name: /sign in with passkey/i })).not.toBeInTheDocument();
  });

  it("triggers loginWithPasskey when clicking passkey button", async () => {
    const loginWithPasskeyMock = vi.fn().mockResolvedValue(undefined);
    vi.mocked(useAuth).mockReturnValue({
      login: loginMock,
      loginWithPasskey: loginWithPasskeyMock,
      passkeyLoginEnabled: true,
    } as unknown as ReturnType<typeof useAuth>);

    render(<Login />);
    const passkeyBtn = screen.getByRole("button", { name: /sign in with passkey/i });
    fireEvent.click(passkeyBtn);

    await waitFor(() => {
      expect(loginWithPasskeyMock).toHaveBeenCalledWith(false);
    });
  });

  it("handles passkey login error by displaying error banner", async () => {
    const loginWithPasskeyMock = vi.fn().mockRejectedValue(new Error("unexpected failure"));
    vi.mocked(useAuth).mockReturnValue({
      login: loginMock,
      loginWithPasskey: loginWithPasskeyMock,
      passkeyLoginEnabled: true,
    } as unknown as ReturnType<typeof useAuth>);

    render(<Login />);
    const passkeyBtn = screen.getByRole("button", { name: /sign in with passkey/i });
    fireEvent.click(passkeyBtn);

    await waitFor(() => {
      expect(screen.getByText(/Passkey sign-in failed\. Try again/)).toBeInTheDocument();
    });
  });

  describe("passkey sign-in", () => {
    const loginWithPasskeyMock = vi.fn();
    const withPasskeys = () =>
      vi.mocked(useAuth).mockReturnValue({
        login: loginMock,
        loginWithPasskey: loginWithPasskeyMock,
        passkeyLoginEnabled: true,
      } as unknown as ReturnType<typeof useAuth>);

    afterEach(() => {
      Object.defineProperty(window, "isSecureContext", { value: true, configurable: true });
    });

    it("stays quiet when the user cancels the browser prompt", async () => {
      withPasskeys();
      loginWithPasskeyMock.mockRejectedValueOnce(new DOMException("denied", "NotAllowedError"));
      render(<Login />);
      fireEvent.click(screen.getByRole("button", { name: /sign in with passkey/i }));
      await waitFor(() => expect(loginWithPasskeyMock).toHaveBeenCalled());
      await waitFor(() =>
        expect(screen.getByRole("button", { name: /sign in with passkey/i })).toBeEnabled(),
      );
      expect(document.querySelector(".auth-error-banner")).toBeNull();
    });

    it("shows the server's generic failure message", async () => {
      withPasskeys();
      loginWithPasskeyMock.mockRejectedValueOnce(
        new ConnectError("passkey sign-in failed", Code.Unauthenticated),
      );
      render(<Login />);
      fireEvent.click(screen.getByRole("button", { name: /sign in with passkey/i }));
      expect(await screen.findByText("passkey sign-in failed")).toBeInTheDocument();
    });

    it("shows rate-limit info with its retry time", async () => {
      withPasskeys();
      loginWithPasskeyMock.mockRejectedValueOnce(
        new ConnectError(
          "too many failed login attempts, please try again in 42 seconds",
          Code.ResourceExhausted,
        ),
      );
      render(<Login />);
      fireEvent.click(screen.getByRole("button", { name: /sign in with passkey/i }));
      expect(await screen.findByText(/try again in 42 seconds/)).toBeInTheDocument();
    });

    it("hides library or browser detail behind a generic message", async () => {
      withPasskeys();
      loginWithPasskeyMock.mockRejectedValueOnce(new TypeError("raw browser detail"));
      render(<Login />);
      fireEvent.click(screen.getByRole("button", { name: /sign in with passkey/i }));
      expect(await screen.findByText(/Passkey sign-in failed\. Try again/)).toBeInTheDocument();
      expect(screen.queryByText(/raw browser detail/)).not.toBeInTheDocument();
    });

    it("disables the passkey button with an explanation in an insecure context", () => {
      Object.defineProperty(window, "isSecureContext", { value: false, configurable: true });
      withPasskeys();
      render(<Login />);
      expect(screen.getByRole("button", { name: /sign in with passkey/i })).toBeDisabled();
      expect(screen.getByRole("note")).toHaveTextContent(/secure connection/);
    });
  });
});
