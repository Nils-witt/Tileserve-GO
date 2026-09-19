import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from 'react';
import { apiJson } from '../../api/client';
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
      setGrants(await apiJson<MapPermission[]>(`/maps/${map.uuid}/permissions`));
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
