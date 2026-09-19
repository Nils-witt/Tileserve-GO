import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from 'react';
import { api } from '../../api/ApiClient';
import type { MapGroupPermission, MapSummary } from '../../api/types';

interface MapGroupPermissionsData {
  groupGrants: MapGroupPermission[];
  error: string | null;
  reloadGroupPermissions: () => Promise<void>;
}

const MapGroupPermissionsContext = createContext<MapGroupPermissionsData | null>(null);

export function MapGroupPermissionsProvider({
  map,
  children,
}: {
  map: MapSummary;
  children: ReactNode;
}) {
  const [groupGrants, setGroupGrants] = useState<MapGroupPermission[]>([]);
  const [error, setError] = useState<string | null>(null);

  const reloadGroupPermissions = useCallback(async () => {
    setError(null);
    try {
      setGroupGrants(await api.listMapGroupPermissions(map.uuid));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }, [map]);

  useEffect(() => {
    reloadGroupPermissions();
  }, [reloadGroupPermissions]);

  const value = useMemo<MapGroupPermissionsData>(
    () => ({ groupGrants, error, reloadGroupPermissions }),
    [groupGrants, error, reloadGroupPermissions],
  );

  return (
    <MapGroupPermissionsContext.Provider value={value}>
      {children}
    </MapGroupPermissionsContext.Provider>
  );
}

export function useMapGroupPermissions(): MapGroupPermissionsData {
  const ctx = useContext(MapGroupPermissionsContext);
  if (!ctx)
    throw new Error('useMapGroupPermissions must be used within MapGroupPermissionsProvider');
  return ctx;
}
