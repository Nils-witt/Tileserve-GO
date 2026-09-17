import { useEffect, useState, type SubmitEvent } from "react";
import {useLocation, useNavigate} from "react-router-dom";
import { useAuth } from "../auth/AuthContext";
import { useAuthMethods } from "../auth/useAuthMethods";
import Footer from "../components/Footer";
import ErrorBanner from "../components/ErrorBanner";

export default function LoginPage() {
  const auth = useAuth();
  const authMethods = useAuthMethods();
  const routerLocation = useLocation();
  const from = (routerLocation.state as { from?: string } | null)?.from ?? null;

  const navigate = useNavigate();

  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (auth.sessionMessage) {
      setError(auth.sessionMessage);
      auth.clearSessionMessage();
    }
    // Only re-run when the session-expired message actually changes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [auth.sessionMessage]);

  const handleSubmit = async (e: SubmitEvent<HTMLFormElement>) => {
    e.preventDefault();
    setError(null);
    setSubmitting(true);
    try {
      await auth.login(username, password);
      await navigate(from ?? "/ui", { replace: true });
    } catch (err) {
      setError(err instanceof Error ? err.message : "Login failed");
    } finally {
      setSubmitting(false);
    }
  };

  const handleSSO = () => {
    window.location.href = "/login/oidc?redirect=" + encodeURIComponent(from ?? "/login");
  };


  return (
    <div className="login-page">
      <div className="card login-card">
        <h1>Sign in to get a token</h1>
        <form onSubmit={handleSubmit}>
          <div className="field" style={{ marginBottom: "0.9rem" }}>
            <label htmlFor="username">Username</label>
            <input
              id="username"
              autoComplete="username"
              required
              value={username}
              onChange={(e) => setUsername(e.target.value)}
            />
          </div>
          <div className="field" style={{ marginBottom: "0.9rem" }}>
            <label htmlFor="password">Password</label>
            <input
              id="password"
              type="password"
              autoComplete="current-password"
              required
              value={password}
              onChange={(e) => setPassword(e.target.value)}
            />
          </div>
          <button type="submit" disabled={submitting} style={{ width: "100%" }}>
            {submitting ? "Signing in…" : "Sign in"}
          </button>
        </form>
        {authMethods.oidc && (
          <button type="button" className="secondary" style={{ width: "100%", marginTop: "0.5rem" }} onClick={handleSSO}>
            Sign in with SSO
          </button>
        )}
        <ErrorBanner message={error} />
      </div>
      <Footer />
    </div>
  );
}
