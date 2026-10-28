import { FormEvent, useState } from "react";
import { useNavigate } from "react-router-dom";
import { api } from "../api/client";
import {
  Eye,
  EyeSlash,
  Key,
  WarningCircle,
  CheckCircle,
} from "@phosphor-icons/react";

export default function ChangePassword() {
  const navigate = useNavigate();
  const [currentPassword, setCurrentPassword] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [showCurrent, setShowCurrent] = useState(false);
  const [showNew, setShowNew] = useState(false);
  const [showConfirm, setShowConfirm] = useState(false);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setError("");

    if (newPassword.length < 8) {
      setError("New password must be at least 8 characters");
      return;
    }
    if (newPassword !== confirmPassword) {
      setError("Passwords do not match");
      return;
    }
    if (newPassword === currentPassword) {
      setError("New password must be different from current password");
      return;
    }

    setLoading(true);
    try {
      await api.changePassword(currentPassword, newPassword);
      navigate("/tenants", { replace: true });
    } catch (err) {
      setError(
        err instanceof Error ? err.message : "Failed to change password"
      );
    } finally {
      setLoading(false);
    }
  }

  // Password strength indicators
  const hasLength = newPassword.length >= 8;
  const hasDifferent = newPassword !== currentPassword && newPassword.length > 0;
  const passwordsMatch = newPassword === confirmPassword && confirmPassword.length > 0;

  return (
    <div className="flex min-h-screen items-center justify-center bg-surface-soft p-6">
      <div className="w-full max-w-md">
        {/* Logo and heading */}
        <div className="mb-8 text-center">
          <div className="mx-auto mb-4 flex h-12 w-12 items-center justify-center rounded-lg bg-ink">
            <span className="text-title-md font-medium text-canvas">K</span>
          </div>
          <h1 className="text-display-md text-ink">Change Password</h1>
          <p className="mt-2 text-body-md text-muted">
            You are using the default password. Please set a new password to
            continue.
          </p>
        </div>

        {/* Form card */}
        <div className="card p-8">
          {error && (
            <div className="mb-6 flex items-start gap-3 rounded-lg border border-error-bg bg-error-bg px-4 py-3">
              <WarningCircle
                size={20}
                weight="fill"
                className="shrink-0 text-error-text"
              />
              <span className="text-body-md text-error-text">{error}</span>
            </div>
          )}

          <form onSubmit={onSubmit} className="space-y-5">
            <div>
              <label htmlFor="current-password" className="label">
                Current Password
              </label>
              <div className="relative">
                <input
                  id="current-password"
                  type={showCurrent ? "text" : "password"}
                  className="input-field pr-12"
                  value={currentPassword}
                  onChange={(e) => setCurrentPassword(e.target.value)}
                  required
                  autoFocus
                  autoComplete="current-password"
                />
                <button
                  type="button"
                  onClick={() => setShowCurrent(!showCurrent)}
                  className="absolute right-3 top-1/2 -translate-y-1/2 rounded p-1 text-muted transition-colors hover:text-ink"
                  aria-label={
                    showCurrent ? "Hide password" : "Show password"
                  }
                >
                  {showCurrent ? (
                    <EyeSlash size={20} weight="regular" />
                  ) : (
                    <Eye size={20} weight="regular" />
                  )}
                </button>
              </div>
            </div>

            <div>
              <label htmlFor="new-password" className="label">
                New Password
              </label>
              <div className="relative">
                <input
                  id="new-password"
                  type={showNew ? "text" : "password"}
                  className="input-field pr-12"
                  value={newPassword}
                  onChange={(e) => setNewPassword(e.target.value)}
                  required
                  minLength={8}
                  autoComplete="new-password"
                />
                <button
                  type="button"
                  onClick={() => setShowNew(!showNew)}
                  className="absolute right-3 top-1/2 -translate-y-1/2 rounded p-1 text-muted transition-colors hover:text-ink"
                  aria-label={showNew ? "Hide password" : "Show password"}
                >
                  {showNew ? (
                    <EyeSlash size={20} weight="regular" />
                  ) : (
                    <Eye size={20} weight="regular" />
                  )}
                </button>
              </div>
              <p className="mt-2 text-body-sm text-muted">
                Minimum 8 characters
              </p>
            </div>

            <div>
              <label htmlFor="confirm-password" className="label">
                Confirm New Password
              </label>
              <div className="relative">
                <input
                  id="confirm-password"
                  type={showConfirm ? "text" : "password"}
                  className="input-field pr-12"
                  value={confirmPassword}
                  onChange={(e) => setConfirmPassword(e.target.value)}
                  required
                  minLength={8}
                  autoComplete="new-password"
                />
                <button
                  type="button"
                  onClick={() => setShowConfirm(!showConfirm)}
                  className="absolute right-3 top-1/2 -translate-y-1/2 rounded p-1 text-muted transition-colors hover:text-ink"
                  aria-label={
                    showConfirm ? "Hide password" : "Show password"
                  }
                >
                  {showConfirm ? (
                    <EyeSlash size={20} weight="regular" />
                  ) : (
                    <Eye size={20} weight="regular" />
                  )}
                </button>
              </div>
            </div>

            {/* Password requirements */}
            {newPassword.length > 0 && (
              <div className="rounded-lg border border-hairline bg-surface-soft p-4">
                <p className="mb-2 text-caption text-ink">Requirements</p>
                <ul className="space-y-1.5">
                  <Requirement met={hasLength} label="At least 8 characters" />
                  <Requirement
                    met={hasDifferent}
                    label="Different from current password"
                  />
                  <Requirement met={passwordsMatch} label="Passwords match" />
                </ul>
              </div>
            )}

            <button
              type="submit"
              disabled={loading}
              className="btn-primary w-full gap-2"
            >
              {loading ? (
                <>
                  <span className="spinner" />
                  <span>Changing password...</span>
                </>
              ) : (
                <>
                  <Key size={18} weight="bold" />
                  <span>Change Password</span>
                </>
              )}
            </button>
          </form>
        </div>
      </div>
    </div>
  );
}

function Requirement({ met, label }: { met: boolean; label: string }) {
  return (
    <li className="flex items-center gap-2">
      <CheckCircle
        size={14}
        weight="fill"
        className={met ? "text-status-paid" : "text-muted"}
      />
      <span
        className={`text-body-sm ${met ? "text-status-paid" : "text-muted"}`}
      >
        {label}
      </span>
    </li>
  );
}