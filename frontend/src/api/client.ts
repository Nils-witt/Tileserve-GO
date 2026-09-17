// Fetch wrapper for the tileserve-go JSON API: attaches the bearer token
// (stored in sessionStorage, matching the previous vanilla-JS UI's
// TOKEN_KEY/USER_KEY), and on a 401 clears the session and notifies
// whoever registered onUnauthorized (the AuthContext) so it can redirect to
// the login page.

const TOKEN_KEY = "tileserve_token";
const USER_KEY = "tileserve_username";

export function getToken(): string | null {
  return sessionStorage.getItem(TOKEN_KEY);
}

export function getStoredUsername(): string | null {
  return sessionStorage.getItem(USER_KEY);
}

export function setSession(token: string, username: string): void {
  sessionStorage.setItem(TOKEN_KEY, token);
  sessionStorage.setItem(USER_KEY, username);
}

export function clearSession(): void {
  sessionStorage.removeItem(TOKEN_KEY);
  sessionStorage.removeItem(USER_KEY);
}

let onUnauthorized: (() => void) | null = null;

export function setUnauthorizedHandler(handler: () => void): void {
  onUnauthorized = handler;
}

export class ApiError extends Error {}

/** apiFetch mirrors the previous UI's `api()` helper: attaches the bearer
 * token, clears the session and notifies the auth layer on 401, and throws
 * on any other non-OK response. */
export async function apiFetch(path: string, options: RequestInit = {}): Promise<Response> {
  const headers = new Headers(options.headers);
  headers.set("Authorization", "Bearer " + (getToken() ?? ""));

  const res = await fetch(path, { ...options, headers });

  if (res.status === 401) {
    clearSession();
    onUnauthorized?.();
    throw new ApiError("unauthorized");
  }

  if (!res.ok) {
    const text = await res.text();
    throw new ApiError(text || `request failed with status ${res.status}`);
  }

  return res;
}

export async function apiJson<T>(path: string, options: RequestInit = {}): Promise<T> {
  const res = await apiFetch(path, options);
  return (await res.json()) as T;
}

export function apiPostJSON<T>(path: string, body: unknown, method = "POST"): Promise<T> {
  return apiJson<T>(path, {
    method,
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
}
