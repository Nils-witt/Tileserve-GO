// REST API response/request shapes, matching the Go handlers under
// internal/webserver exactly (field names are JSON tags, not Go names).

export interface MapSummary {
  uuid: string;
  name: string;
  currentVersion: string;
  visibleToAll: boolean;
  anonymousAllowed: boolean;
  owner: string;
  createdAt: string;
  createdBy: string;
  updatedAt: string;
  updatedBy: string;
}

export interface MapVersion {
  version: string;
  createdAt: string;
  createdBy: string;
}

export interface MapBounds {
  centerLng: number;
  centerLat: number;
  minZoom: number;
}

export interface User {
  username: string;
  canCreate: boolean;
  canEdit: boolean;
  canDelete: boolean;
  canEditGeoObjects: boolean;
  canDeleteGeoObjects: boolean;
  canViewAll: boolean;
  isAdmin: boolean;
  createdAt: string;
}

export interface Group {
  id: string;
  name: string;
  canCreate: boolean;
  canEdit: boolean;
  canDelete: boolean;
  canEditGeoObjects: boolean;
  canDeleteGeoObjects: boolean;
  canViewAll: boolean;
  isAdmin: boolean;
  ldapGroupDn?: string;
  oidcGroupClaim?: string;
  createdAt: string;
}

export interface MapPermission {
  username: string;
  canView: boolean;
  canEdit: boolean;
  canDelete: boolean;
  canEditGeoObjects: boolean;
  canDeleteGeoObjects: boolean;
}

export interface MapGroupPermission {
  groupId: string;
  canView: boolean;
  canEdit: boolean;
  canDelete: boolean;
  canEditGeoObjects: boolean;
  canDeleteGeoObjects: boolean;
}

export interface MapAlias {
  alias: string;
  version: string;
  updatedAt: string;
  updatedBy: string;
}

export interface GeoObject {
  uuid: string;
  name: string;
  externalId?: string;
  latitude: number;
  longitude: number;
  street?: string;
  housenumber?: string;
  postcode?: string;
  city?: string;
  cityDistrict?: string;
}

export interface ApiKey {
  id: string;
  name?: string;
  createdAt: string;
  createdBy: string;
  lastUsedAt?: string;
  scoped: boolean;
}

export interface GeneratedKeyPair {
  publicKeyPem: string;
  privateKeyPem: string;
}

export interface ApiKeyScope {
  mapUuid: string;
  versions?: string[];
}

export interface SyncRemote {
  id: string;
  name: string;
  baseUrl: string;
  remoteApiKeyId: string;
  pollIntervalSec: number;
  enabled: boolean;
  syncAllMaps: boolean;
  syncNewMaps: boolean;
  syncGeoObjects: boolean;
  lastSyncAt?: string;
  lastSyncStatus?: string;
  lastSyncError?: string;
}

export interface SyncLogEntry {
  time: string;
  level: string;
  message: string;
}

export interface RemoteMap {
  uuid: string;
  name: string;
}

export interface AuditLogEntry {
  occurredAt: string;
  actor: string;
  action: string;
  entityType: string;
  entityId: string;
  detail: string;
}

export interface CurrentPermissions {
  isAdmin: boolean;
}

export interface AuthMethods {
  oidc: boolean;
}

export interface VersionInfo {
  version?: string;
  commit: string;
}

export interface LoginResponse {
  token: string;
  refresh_token: string;
}
