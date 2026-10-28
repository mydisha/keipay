import { FormEvent, useState } from "react";
import { useNavigate } from "react-router-dom";
import { api, setToken } from "../api/client";
import { Eye, EyeSlash, SignIn, WarningCircle } from "@phosphor-icons/react";

export default function Login() {
  const navigate = useNavigate();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [showPassword, setShowPassword] = useState(false);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setError("");
    setLoading(true);
    try {
      const result = await api.login(username, password);
      setToken(result.token);
      if (result.must_change_password) {
        navigate("/change-password", { replace: true });
      } else {
        navigate("/tenants", { replace: true });
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "Invalid credentials");
    } finally {
      setLoading(false);
    }
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-surface-soft p-6">
      <div className="w-full max-w-md">
        {/* Logo and heading */}
        <div className="mb-8 text-center">
          <div className="mx-auto mb-4 flex h-12 w-12 items-center justify-center rounded-lg bg-ink">
            <span className="text-title-md font-medium text-canvas">K</span>
          </div>
          <h1 className="text-display-md text-ink">KeiPay</h1>
          <p className="mt-2 text-body-md text-muted">
            Sign in to the admin dashboard
          </p>
        </div>

        {/* Login form */}
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
              <label htmlFor="username" className="label">
                Username
              </label>
              <input
                id="username"
                type="text"
                className="input-field"
                value={username}
                onChange={(e) => setUsername(e.target.value)}
                placeholder="admin"
                autoFocus
                required
                autoComplete="username"
              />
            </div>

            <div>
              <label htmlFor="password" className="label">
                Password
              </label>
              <div className="relative">
                <input
                  id="password"
                  type={showPassword ? "text" : "password"}
                  className="input-field pr-12"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  placeholder="Enter your password"
                  required
                  autoComplete="current-password"
                />
                <button
                  type="button"
                  onClick={() => setShowPassword(!showPassword)}
                  className="absolute right-3 top-1/2 -translate-y-1/2 rounded p-1 text-muted transition-colors hover:text-ink"
                  aria-label={showPassword ? "Hide password" : "Show password"}
                >
                  {showPassword ? (
                    <EyeSlash size={20} weight="regular" />
                  ) : (
                    <Eye size={20} weight="regular" />
                  )}
                </button>
              </div>
            </div>

            <button
              type="submit"
              disabled={loading}
              className="btn-primary w-full gap-2"
            >
              {loading ? (
                <>
                  <span className="spinner" />
                  <span>Signing in...</span>
                </>
              ) : (
                <>
                  <SignIn size={18} weight="bold" />
                  <span>Sign in</span>
                </>
              )}
            </button>
          </form>
        </div>

        {/* Footer */}
        <p className="mt-6 text-center text-body-sm text-muted">
          Contact your administrator if you need access
        </p>
      </div>
    </div>
  );
}