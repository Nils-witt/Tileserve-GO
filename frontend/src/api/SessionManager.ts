const TOKEN_KEY = 'tileserve_token';
const USER_KEY = 'tileserve_username';

export class SessionManager {
  static getToken(): string | null {
    return sessionStorage.getItem(TOKEN_KEY);
  }

  static getStoredUsername(): string | null {
    return sessionStorage.getItem(USER_KEY);
  }

  static setSession(token: string, username: string): void {
    sessionStorage.setItem(TOKEN_KEY, token);
    sessionStorage.setItem(USER_KEY, username);
  }

  static clearSession(): void {
    sessionStorage.removeItem(TOKEN_KEY);
    sessionStorage.removeItem(USER_KEY);
  }
}
