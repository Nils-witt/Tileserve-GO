// The one place that talks to the tileserve-go HTTP API. It owns:
//   - the session (bearer token + username, kept in sessionStorage under the
//     same keys as the previous vanilla-JS UI),
//   - request plumbing (auth header, JSON bodies, error mapping), and on a
//     401 clears the session and notifies whoever registered
//     setUnauthorizedHandler (the AuthContext) so it can redirect to login,
//   - one typed method per endpoint, so callers never build URLs or
//     serialize bodies themselves.

import type {
  AccountPermissions,
  ApiKey,
  ApiKeyScope,
  AuditLogEntry,
  AuditLogQuery,
  AuthMethods,
  CurrentPermissions,
  GeneratedKeyPair,
  GeoObject,
  Group,
  GroupInput,
  LoginResponse,
  MapAlias,
  MapBounds,
  MapGrant,
  MapGroupPermission,
  MapInput,
  MapPermission,
  MapSummary,
  MapVersion,
  RemoteMap,
  SyncLogEntry,
  SyncRemote,
  SyncRemoteInput,
  User,
  VersionInfo,
} from './types';

const TOKEN_KEY = 'tileserve_token';
const USER_KEY = 'tileserve_username';

export class ApiError extends Error {}

export interface UploadProgress {
  loaded: number;
  total: number;
}

export interface UploadResult {
  ok: boolean;
  status: number;
  unauthorized: boolean;
  errorText?: string;
}

const enc = encodeURIComponent;

export class ApiClient {
  private onUnauthorized: (() => void) | null = null;
  private readonly storage: Storage;

  constructor(storage: Storage = sessionStorage) {
    this.storage = storage;
  }

  // ---- session -----------------------------------------------------------

  getToken(): string | null {
    return this.storage.getItem(TOKEN_KEY);
  }

  getStoredUsername(): string | null {
    return this.storage.getItem(USER_KEY);
  }

  setSession(token: string, username: string): void {
    this.storage.setItem(TOKEN_KEY, token);
    this.storage.setItem(USER_KEY, username);
  }

  clearSession(): void {
    this.storage.removeItem(TOKEN_KEY);
    this.storage.removeItem(USER_KEY);
  }

  setUnauthorizedHandler(handler: () => void): void {
    this.onUnauthorized = handler;
  }

  private handleUnauthorized(): void {
    this.clearSession();
    this.onUnauthorized?.();
  }

  // ---- request plumbing --------------------------------------------------

  /** Attaches the bearer token, handles 401 (clear session + notify), and
   * throws an ApiError carrying the response text on any other non-OK. */
  private async request(path: string, options: RequestInit = {}): Promise<Response> {
    const headers = new Headers(options.headers);
    headers.set('Authorization', 'Bearer ' + (this.getToken() ?? ''));

    const res = await fetch(path, { ...options, headers });

    if (res.status === 401) {
      this.handleUnauthorized();
      throw new ApiError('unauthorized');
    }

    if (!res.ok) {
      const text = await res.text();
      throw new ApiError(text || `request failed with status ${res.status}`);
    }

    return res;
  }

  private async getJson<T>(path: string): Promise<T> {
    const res = await this.request(path);
    return (await res.json()) as T;
  }

  private sendJson(path: string, method: string, body: unknown): Promise<Response> {
    return this.request(path, {
      method,
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    });
  }

  private async sendJsonForJson<T>(path: string, method: string, body: unknown): Promise<T> {
    const res = await this.sendJson(path, method, body);
    return (await res.json()) as T;
  }

  private async del(path: string): Promise<void> {
    await this.request(path, { method: 'DELETE' });
  }

  // ---- unauthenticated ---------------------------------------------------

  /** POST /login. On success stores the session and returns the issued token. */
  async login(username: string, password: string, ttlSeconds?: number): Promise<string> {
    let res: Response;
    try {
      res = await fetch('/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          username,
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
    this.setSession(data.token, username);
    return data.token;
  }

  async getAuthMethods(): Promise<AuthMethods> {
    const res = await fetch('/auth/methods');
    return (await res.json()) as AuthMethods;
  }

  async getVersion(): Promise<VersionInfo> {
    const res = await fetch('/version');
    return (await res.json()) as VersionInfo;
  }

  // ---- current user / server --------------------------------------------

  /** The caller's own effective (group-merged) permissions. */
  getCurrentPermissions(): Promise<CurrentPermissions> {
    return this.getJson('/permissions/me');
  }

  getServerPublicKey(): Promise<{ publicKeyPem: string }> {
    return this.getJson('/server/public-key');
  }

  generateKeyPair(): Promise<GeneratedKeyPair> {
    return this.sendJsonForJson('/keys/generate', 'POST', {});
  }

  // ---- maps --------------------------------------------------------------

  listMaps(): Promise<MapSummary[]> {
    return this.getJson('/maps');
  }

  async createMap(input: MapInput): Promise<void> {
    await this.sendJson('/maps', 'POST', input);
  }

  async updateMap(mapUuid: string, input: MapInput): Promise<void> {
    await this.sendJson(`/maps/${mapUuid}`, 'PUT', input);
  }

  deleteMap(mapUuid: string): Promise<void> {
    return this.del(`/maps/${mapUuid}`);
  }

  async transferMapOwner(mapUuid: string, owner: string): Promise<void> {
    await this.sendJson(`/maps/${mapUuid}/owner`, 'PUT', { owner });
  }

  listMapVersions(mapUuid: string): Promise<MapVersion[]> {
    return this.getJson(`/maps/${mapUuid}/versions`);
  }

  getMapBounds(mapUuid: string, version: string): Promise<MapBounds> {
    return this.getJson(`/maps/${mapUuid}/version/${enc(version)}/bounds`);
  }

  async downloadMapVersion(mapUuid: string, version: string): Promise<Blob> {
    const res = await this.request(`/maps/${mapUuid}/version/${enc(version)}/download`);
    return res.blob();
  }

  /** Tile URL template for a map version, with the session token in the query
   * string since map libraries can't attach an Authorization header to tile
   * requests. */
  mapTileUrl(mapUuid: string, version: string): string {
    return (
      window.location.origin +
      `/maps/${mapUuid}/version/${enc(version)}/{z}/{x}/{y}.png?token=` +
      enc(this.getToken() ?? '')
    );
  }

  /** XHR-based upload with progress events: fetch() can't report upload
   * progress. Resolves (never rejects) with the outcome. */
  uploadMapVersion(
    mapUuid: string,
    file: File,
    onProgress: (progress: UploadProgress) => void,
  ): Promise<UploadResult> {
    return new Promise((resolve) => {
      const xhr = new XMLHttpRequest();
      xhr.open('POST', `/maps/${mapUuid}/upload`);
      xhr.setRequestHeader('Authorization', 'Bearer ' + (this.getToken() ?? ''));
      xhr.setRequestHeader('Content-Type', file.type || 'application/octet-stream');

      xhr.upload.onprogress = (e) => {
        if (!e.lengthComputable) return;
        onProgress({ loaded: e.loaded, total: e.total });
      };

      xhr.onload = () => {
        if (xhr.status === 401) {
          this.handleUnauthorized();
          resolve({ ok: false, status: xhr.status, unauthorized: true });
          return;
        }
        if (xhr.status < 200 || xhr.status >= 300) {
          resolve({
            ok: false,
            status: xhr.status,
            unauthorized: false,
            errorText: xhr.responseText,
          });
          return;
        }
        resolve({ ok: true, status: xhr.status, unauthorized: false });
      };

      xhr.onerror = () => {
        resolve({ ok: false, status: 0, unauthorized: false, errorText: 'upload failed' });
      };

      xhr.send(file);
    });
  }

  // ---- map aliases -------------------------------------------------------

  listMapAliases(mapUuid: string): Promise<MapAlias[]> {
    return this.getJson(`/maps/${mapUuid}/aliases`);
  }

  async setMapAlias(mapUuid: string, alias: string, version: string): Promise<void> {
    await this.sendJson(`/maps/${mapUuid}/aliases/${enc(alias)}`, 'PUT', { version });
  }

  deleteMapAlias(mapUuid: string, alias: string): Promise<void> {
    return this.del(`/maps/${mapUuid}/aliases/${enc(alias)}`);
  }

  // ---- map permissions ---------------------------------------------------

  listMapPermissions(mapUuid: string): Promise<MapPermission[]> {
    return this.getJson(`/maps/${mapUuid}/permissions`);
  }

  async setMapPermission(mapUuid: string, username: string, grant: MapGrant): Promise<void> {
    await this.sendJson(`/maps/${mapUuid}/permissions/${enc(username)}`, 'PUT', grant);
  }

  deleteMapPermission(mapUuid: string, username: string): Promise<void> {
    return this.del(`/maps/${mapUuid}/permissions/${enc(username)}`);
  }

  listMapGroupPermissions(mapUuid: string): Promise<MapGroupPermission[]> {
    return this.getJson(`/maps/${mapUuid}/group-permissions`);
  }

  async setMapGroupPermission(mapUuid: string, groupId: string, grant: MapGrant): Promise<void> {
    await this.sendJson(`/maps/${mapUuid}/group-permissions/${enc(groupId)}`, 'PUT', grant);
  }

  deleteMapGroupPermission(mapUuid: string, groupId: string): Promise<void> {
    return this.del(`/maps/${mapUuid}/group-permissions/${enc(groupId)}`);
  }

  // ---- geo objects -------------------------------------------------------

  private geoObjectsPath(mapUuid: string, version: string, suffix = ''): string {
    return `/maps/${mapUuid}/version/${enc(version)}/geo-objects${suffix}`;
  }

  listGeoObjects(mapUuid: string, version: string): Promise<GeoObject[]> {
    return this.getJson(this.geoObjectsPath(mapUuid, version));
  }

  async createGeoObject(
    mapUuid: string,
    version: string,
    payload: Partial<GeoObject>,
  ): Promise<void> {
    await this.sendJson(this.geoObjectsPath(mapUuid, version), 'POST', payload);
  }

  async updateGeoObject(
    mapUuid: string,
    version: string,
    id: string,
    payload: Partial<GeoObject>,
  ): Promise<void> {
    await this.sendJson(this.geoObjectsPath(mapUuid, version, '/' + id), 'PUT', payload);
  }

  deleteGeoObject(mapUuid: string, version: string, id: string): Promise<void> {
    return this.del(this.geoObjectsPath(mapUuid, version, '/' + id));
  }

  // ---- users -------------------------------------------------------------

  listUsers(): Promise<User[]> {
    return this.getJson('/users');
  }

  createUser(input: { username: string; password: string } & AccountPermissions): Promise<User> {
    return this.sendJsonForJson('/users', 'POST', input);
  }

  async updateUser(
    username: string,
    input: AccountPermissions & { password: string },
  ): Promise<void> {
    await this.sendJson(`/users/${enc(username)}`, 'PUT', input);
  }

  deleteUser(username: string): Promise<void> {
    return this.del(`/users/${enc(username)}`);
  }

  // ---- API keys ----------------------------------------------------------

  listApiKeys(username: string): Promise<ApiKey[]> {
    return this.getJson(`/users/${enc(username)}/api-keys`);
  }

  createApiKey(username: string, input: { name: string; publicKeyPem: string }): Promise<ApiKey> {
    return this.sendJsonForJson(`/users/${enc(username)}/api-keys`, 'POST', input);
  }

  deleteApiKey(username: string, keyId: string): Promise<void> {
    return this.del(`/users/${enc(username)}/api-keys/${keyId}`);
  }

  listApiKeyScopes(username: string, keyId: string): Promise<ApiKeyScope[]> {
    return this.getJson(`/users/${enc(username)}/api-keys/${keyId}/scopes`);
  }

  async setApiKeyScope(
    username: string,
    keyId: string,
    mapUuid: string,
    versions: string[] | null,
  ): Promise<void> {
    await this.sendJson(`/users/${enc(username)}/api-keys/${keyId}/scopes/${mapUuid}`, 'PUT', {
      versions,
    });
  }

  deleteApiKeyScope(username: string, keyId: string, mapUuid: string): Promise<void> {
    return this.del(`/users/${enc(username)}/api-keys/${keyId}/scopes/${mapUuid}`);
  }

  /** Removes every scope, leaving the key unscoped. */
  deleteAllApiKeyScopes(username: string, keyId: string): Promise<void> {
    return this.del(`/users/${enc(username)}/api-keys/${keyId}/scopes`);
  }

  // ---- groups ------------------------------------------------------------

  listGroups(): Promise<Group[]> {
    return this.getJson('/groups');
  }

  createGroup(input: GroupInput): Promise<Group> {
    return this.sendJsonForJson('/groups', 'POST', input);
  }

  async updateGroup(groupId: string, input: GroupInput): Promise<void> {
    await this.sendJson(`/groups/${enc(groupId)}`, 'PUT', input);
  }

  deleteGroup(groupId: string): Promise<void> {
    return this.del(`/groups/${enc(groupId)}`);
  }

  // ---- sync remotes ------------------------------------------------------

  listSyncRemotes(): Promise<SyncRemote[]> {
    return this.getJson('/sync/remotes');
  }

  createSyncRemote(input: SyncRemoteInput): Promise<SyncRemote> {
    return this.sendJsonForJson('/sync/remotes', 'POST', input);
  }

  async updateSyncRemote(remoteId: string, input: SyncRemoteInput): Promise<void> {
    await this.sendJson(`/sync/remotes/${remoteId}`, 'PUT', input);
  }

  deleteSyncRemote(remoteId: string): Promise<void> {
    return this.del(`/sync/remotes/${remoteId}`);
  }

  async triggerSyncRemote(remoteId: string): Promise<void> {
    await this.request(`/sync/remotes/${remoteId}/trigger`, { method: 'POST' });
  }

  listSyncRemoteLogs(remoteId: string): Promise<SyncLogEntry[]> {
    return this.getJson(`/sync/remotes/${remoteId}/logs`);
  }

  listRemoteMaps(remoteId: string): Promise<RemoteMap[]> {
    return this.getJson(`/sync/remotes/${remoteId}/remote-maps`);
  }

  listSelectedRemoteMaps(remoteId: string): Promise<string[]> {
    return this.getJson(`/sync/remotes/${remoteId}/selected-maps`);
  }

  // ---- audit log ---------------------------------------------------------

  listAuditLogs(query: AuditLogQuery): Promise<AuditLogEntry[]> {
    const params = new URLSearchParams();
    if (query.actor?.trim()) params.set('actor', query.actor.trim());
    if (query.action?.trim()) params.set('action', query.action.trim());
    if (query.entityType?.trim()) params.set('entityType', query.entityType.trim());
    if (query.entityId?.trim()) params.set('entityId', query.entityId.trim());
    params.set('limit', String(query.limit));
    params.set('offset', String(query.offset));
    return this.getJson('/audit-logs?' + params.toString());
  }
}

/** Shared instance used across the app. */
export const api = new ApiClient();
