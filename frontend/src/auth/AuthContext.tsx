import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from 'react';
import {
  clearSession,
  getStoredUsername,
  getToken,
  setSession,
  setUnauthorizedHandler,
} from '../api/client';
import type { LoginResponse } from '../api/types';

interface AuthState {
  username: string | null;
  isAuthenticated: boolean;
  sessionMessage: string | null;
  /** login performs POST /login and, on success, stores the session and
   * returns the issued token (e.g. so the login page can also display it,
   * matching the previous login.html's "show me a token" behavior). */
  login: (username: string, password: string, ttlSeconds?: number) => Promise<string>;
  logout: () => void;
  /** consumeOIDCRedirect mirrors both previous pages' identical
   * hash-fragment handling: the OIDC callback hands the token (and, for the
   * admin app, the username) back via location.hash rather than a query
   * parameter, so it never reaches a server log or Referer header. */
  clearSessionMessage: () => void;
}

const AuthContext = createContext<AuthState | null>(null);

function consumeOIDCFragment(): { token: string; username: string } | null {
  if (!location.hash) return null;
  const params = new URLSearchParams(location.hash.slice(1));
  const token = params.get('token');
  const username = params.get('username');
  if (!token || !username) return null;
  history.replaceState(null, '', location.pathname + location.search);
  return { token, username };
}

export function AuthProvider({ children }: { children: ReactNode }) {
  const [username, setUsername] = useState<string | null>(() => getStoredUsername());
  const [sessionMessage, setSessionMessage] = useState<string | null>(null);

  useEffect(() => {
    const consumed = consumeOIDCFragment();
    if (consumed) {
      setSession(consumed.token, consumed.username);
      setUsername(consumed.username);
    }
  }, []);

  useEffect(() => {
    setUnauthorizedHandler(() => {
      setUsername(null);
      setSessionMessage('Session expired, please sign in again.');
    });
  }, []);

  const login = useCallback(
    async (loginUsername: string, password: string, ttlSeconds?: number) => {
      let res: Response;
      try {
        res = await fetch('/login', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            username: loginUsername,
            password,
            ...(ttlSeconds ? { ttl_seconds: ttlSeconds } : {}),
          }),
        });
      } catch {
        throw new Error('Login failed');
      }
      if (!res.ok) {
        throw new Error(res.status === 401 ? 'Invalid credentials' : 'Login failed');
      }
      const data = (await res.json()) as LoginResponse;
      setSession(data.token, loginUsername);
      setUsername(loginUsername);
      return data.token;
    },
    [],
  );

  const logout = useCallback(() => {
    clearSession();
    setUsername(null);
  }, []);

  const clearSessionMessage = useCallback(() => setSessionMessage(null), []);

  const value = useMemo<AuthState>(
    () => ({
      username,
      isAuthenticated: !!username && !!getToken(),
      sessionMessage,
      login,
      logout,
      clearSessionMessage,
    }),
    [username, sessionMessage, login, logout, clearSessionMessage],
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth(): AuthState {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error('useAuth must be used within AuthProvider');
  return ctx;
}
