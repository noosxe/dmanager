import { Eye, EyeOff, Loader2, Lock, ShieldAlert, Sparkles, Terminal, User } from "lucide-react";
import type React from "react";
import { useState } from "react";
import { useAuth } from "../hooks/useAuth";

export function Setup() {
  const { setupAdmin } = useAuth();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [showPassword, setShowPassword] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [isSubmitting, setIsSubmitting] = useState(false);

  // Validation errors
  const [validationErrors, setValidationErrors] = useState<{
    username?: string;
    password?: string;
    confirmPassword?: string;
  }>({});

  const validate = () => {
    const errors: { username?: string; password?: string; confirmPassword?: string } = {};
    if (!username.trim()) {
      errors.username = "Username is required";
    } else if (username.length < 3) {
      errors.username = "Username must be at least 3 characters";
    }

    if (!password) {
      errors.password = "Password is required";
    } else if (password.length < 12) {
      errors.password = "Password must be at least 12 characters (passphrases recommended)";
    }

    if (password !== confirmPassword) {
      errors.confirmPassword = "Passwords do not match";
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
      await setupAdmin(username.trim(), password);
    } catch (err: unknown) {
      console.error(err);
      const message = err instanceof Error ? err.message : String(err);
      setError(message || "Failed to initialize administrator account");
    } finally {
      setIsSubmitting(false);
    }
  };

  return (
    <div className="auth-container setup-theme">
      <div className="auth-bg-glow-1"></div>
      <div className="auth-bg-glow-2"></div>

      <div className="auth-card">
        <div className="auth-header">
          <div className="auth-logo">
            <Terminal size={28} className="auth-logo-icon" />
          </div>
          <h1>dmanager</h1>
          <p className="auth-subtitle">Initialize Administrator Account</p>
          <span className="auth-tag">First-Time Setup Mode</span>
        </div>

        {error && (
          <div className="auth-error-banner">
            <ShieldAlert size={18} className="auth-error-icon" />
            <span>{error}</span>
          </div>
        )}

        <form onSubmit={handleSubmit} className="auth-form">
          <div className="form-group">
            <label htmlFor="setup-username">Admin Username</label>
            <div className="input-wrapper">
              <User size={18} className="input-icon" />
              <input
                id="setup-username"
                type="text"
                value={username}
                onChange={(e) => {
                  setUsername(e.target.value);
                  if (validationErrors.username) {
                    setValidationErrors((prev) => ({ ...prev, username: undefined }));
                  }
                }}
                placeholder="Choose admin username"
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
            <label htmlFor="setup-password">Admin Password</label>
            <div className="input-wrapper">
              <Lock size={18} className="input-icon" />
              <input
                id="setup-password"
                type={showPassword ? "text" : "password"}
                value={password}
                onChange={(e) => {
                  setPassword(e.target.value);
                  if (validationErrors.password) {
                    setValidationErrors((prev) => ({ ...prev, password: undefined }));
                  }
                }}
                placeholder="Choose strong password"
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

          <div className="form-group">
            <label htmlFor="setup-confirm-password">Confirm Password</label>
            <div className="input-wrapper">
              <Lock size={18} className="input-icon" />
              <input
                id="setup-confirm-password"
                type={showPassword ? "text" : "password"}
                value={confirmPassword}
                onChange={(e) => {
                  setConfirmPassword(e.target.value);
                  if (validationErrors.confirmPassword) {
                    setValidationErrors((prev) => ({ ...prev, confirmPassword: undefined }));
                  }
                }}
                placeholder="Confirm password"
                maxLength={100}
                disabled={isSubmitting}
                className={validationErrors.confirmPassword ? "input-error" : ""}
              />
            </div>
            {validationErrors.confirmPassword && (
              <span className="error-text">{validationErrors.confirmPassword}</span>
            )}
          </div>

          <button type="submit" className="auth-submit-btn" disabled={isSubmitting}>
            {isSubmitting ? (
              <>
                <Loader2 size={18} className="spinner" />
                <span>Initializing...</span>
              </>
            ) : (
              <>
                <Sparkles size={18} />
                <span>Create Admin Account</span>
              </>
            )}
          </button>
        </form>
      </div>
    </div>
  );
}
