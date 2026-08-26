import { ConnectError } from "@connectrpc/connect";
import { Eye, EyeOff, Loader2, Lock, ShieldAlert, Sparkles, Terminal, User } from "lucide-react";
import type React from "react";
import { useState } from "react";
import { useAuth } from "../hooks/useAuth";

export function Login() {
  const { login } = useAuth();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [rememberMe, setRememberMe] = useState(false);
  const [showPassword, setShowPassword] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [isSubmitting, setIsSubmitting] = useState(false);

  // Validation errors
  const [validationErrors, setValidationErrors] = useState<{
    username?: string;
    password?: string;
  }>({});

  const validate = () => {
    const errors: { username?: string; password?: string } = {};
    if (!username.trim()) {
      errors.username = "Username is required";
    } else if (username.length < 3) {
      errors.username = "Username must be at least 3 characters";
    }

    if (!password) {
      errors.password = "Password is required";
    } else if (password.length < 6) {
      errors.password = "Password must be at least 6 characters";
    }

    setValidationErrors(errors);
    return Object.keys(errors).length === 0;
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);

    if (!validate()) {
      return;
    }

    setIsSubmitting(true);
    try {
      await login(username.trim(), password, rememberMe);
    } catch (err: unknown) {
      console.error(err);
      if (err instanceof ConnectError) {
        setError(err.rawMessage || err.message);
      } else {
        const message = err instanceof Error ? err.message : String(err);
        setError(message || "Invalid username or password");
      }
    } finally {
      setIsSubmitting(false);
    }
  };

  return (
    <div className="auth-container">
      <div className="auth-bg-glow-1"></div>
      <div className="auth-bg-glow-2"></div>

      <div className="auth-card">
        <div className="auth-header">
          <div className="auth-logo">
            <Terminal size={28} className="auth-logo-icon" />
          </div>
          <h1>dmanager</h1>
          <p className="auth-subtitle">Container Management Console</p>
        </div>

        {error && (
          <div className="auth-error-banner">
            <ShieldAlert size={18} className="auth-error-icon" />
            <span>{error}</span>
          </div>
        )}

        <form onSubmit={handleSubmit} className="auth-form">
          <div className="form-group">
            <label htmlFor="username">Username</label>
            <div className="input-wrapper">
              <User size={18} className="input-icon" />
              <input
                id="username"
                type="text"
                value={username}
                onChange={(e) => {
                  setUsername(e.target.value);
                  if (validationErrors.username) {
                    setValidationErrors((prev) => ({ ...prev, username: undefined }));
                  }
                }}
                placeholder="Enter your username"
                maxLength={50}
                disabled={isSubmitting}
                className={validationErrors.username ? "input-error" : ""}
              />
            </div>
            {validationErrors.username && (
              <span className="error-text">{validationErrors.username}</span>
            )}
          </div>

          <div className="form-group">
            <label htmlFor="password">Password</label>
            <div className="input-wrapper">
              <Lock size={18} className="input-icon" />
              <input
                id="password"
                type={showPassword ? "text" : "password"}
                value={password}
                onChange={(e) => {
                  setPassword(e.target.value);
                  if (validationErrors.password) {
                    setValidationErrors((prev) => ({ ...prev, password: undefined }));
                  }
                }}
                placeholder="Enter your password"
                maxLength={100}
                disabled={isSubmitting}
                className={validationErrors.password ? "input-error" : ""}
              />
              <button
                type="button"
                onClick={() => setShowPassword(!showPassword)}
                className="password-toggle"
                disabled={isSubmitting}
                aria-label={showPassword ? "Hide password" : "Show password"}
              >
                {showPassword ? <EyeOff size={18} /> : <Eye size={18} />}
              </button>
            </div>
            {validationErrors.password && (
              <span className="error-text">{validationErrors.password}</span>
            )}
          </div>

          <label className="auth-checkbox-group" htmlFor="remember-me">
            <input
              id="remember-me"
              type="checkbox"
              checked={rememberMe}
              onChange={(e) => setRememberMe(e.target.checked)}
              disabled={isSubmitting}
            />
            <span className="auth-checkbox-label">Remember me</span>
          </label>

          <button type="submit" className="auth-submit-btn" disabled={isSubmitting}>
            {isSubmitting ? (
              <>
                <Loader2 size={18} className="spinner" />
                <span>Authenticating...</span>
              </>
            ) : (
              <>
                <Sparkles size={18} />
                <span>Sign In</span>
              </>
            )}
          </button>
        </form>
      </div>
    </div>
  );
}
