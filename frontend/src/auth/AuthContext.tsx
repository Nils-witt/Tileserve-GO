import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from 'react';
import { SessionManager } from '../api/SessionManager.ts';
import { ApiClient } from '../api/ApiClient.ts';

interface AuthState {
  username: string | null;
  isAuthenticated: boolean;
  sessionMessage: string | null;
  /** login performs POST /login and, on success, stores the session and
   * returns the issued token (e.g. so the login page can also display it,
   * matching the previous login.html's "show me a token" behavior). */
  login: (
    username: string,
    password: string,
    ttlSeconds?: number,
  ) => Promise<{ token: string; username: string }>;
  logout: () => void;
  /** consumeOIDCRedirect mirrors both previous pages' identical
   * hash-fragment handling: the OIDC callback hands the token (and, for the
   * admin app, the username) back via location.hash rather than a query
   * parameter, so it never reaches a server log or Referer header. */
  clearSessionMessage: () => void;
  token: string | null;
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
  const [username, setUsername] = useState<string | null>(() => SessionManager.getStoredUsername());
  const [sessionMessage, setSessionMessage] = useState<string | null>(null);
  const [ssoComplete, setSsoComplete] = useState<boolean>(false);
  const [token, setToken] = useState<string | null>(() => SessionManager.getToken());

  useEffect(() => {
    const consumed = consumeOIDCFragment();
    if (consumed) {
      SessionManager.setSession(consumed.token, consumed.username);
      setUsername(consumed.username);
      setToken(consumed.token);
      window.location.href = '/ui';
    }
    setSsoComplete(true);
  }, []);

  const login = useCallback(
    async (loginUsername: string, password: string, ttlSeconds?: number) => {
      const pApi = new ApiClient({
        token: null,
      });
      const issuedToken = await pApi.login(loginUsername, password, ttlSeconds);
      SessionManager.setSession(issuedToken.token, issuedToken.username);
      setUsername(issuedToken.username);
      setToken(issuedToken.token);
      return issuedToken;
    },
    [],
  );

  const logout = useCallback(() => {
    SessionManager.clearSession();
    setUsername(null);
    setToken(null);
  }, []);

  const clearSessionMessage = useCallback(() => setSessionMessage(null), []);

  const value = useMemo<AuthState>(
    () => ({
      username,
      isAuthenticated: !!username && !!SessionManager.getToken(),
      sessionMessage,
      login,
      logout,
      clearSessionMessage,
      token,
    }),
    [username, sessionMessage, login, logout, clearSessionMessage, token],
  );

  if (!ssoComplete) {
    return <div>Loading Auth...</div>;
  }

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth(): AuthState {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error('useAuth must be used within AuthProvider');
  return ctx;
}
