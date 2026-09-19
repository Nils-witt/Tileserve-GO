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
import type { MapPermission, MapSummary } from '../../api/types';

interface MapPermissionsData {
  grants: MapPermission[];
  error: string | null;
  reloadPermissions: () => Promise<void>;
}

const MapPermissionsContext = createContext<MapPermissionsData | null>(null);

export function MapPermissionsProvider({
  map,
  children,
}: {
  map: MapSummary;
  children: ReactNode;
}) {
  const [grants, setGrants] = useState<MapPermission[]>([]);
  const [error, setError] = useState<string | null>(null);

  const reloadPermissions = useCallback(async () => {
    setError(null);
    try {
      setGrants(await api.listMapPermissions(map.uuid));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }, [map]);

  useEffect(() => {
    reloadPermissions();
  }, [reloadPermissions]);

  const value = useMemo<MapPermissionsData>(
    () => ({ grants, error, reloadPermissions }),
    [grants, error, reloadPermissions],
  );

  return <MapPermissionsContext.Provider value={value}>{children}</MapPermissionsContext.Provider>;
}

export function useMapPermissions(): MapPermissionsData {
  const ctx = useContext(MapPermissionsContext);
  if (!ctx) throw new Error('useMapPermissions must be used within MapPermissionsProvider');
  return ctx;
}
