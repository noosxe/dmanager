import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
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
      expect(loginMock).toHaveBeenCalledWith("admin", "password123");
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
});
