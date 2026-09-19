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
import type { MapSummary, MapVersion } from '../../api/types';

interface MapVersionsData {
  versions: MapVersion[];
  error: string | null;
  reloadVersions: () => Promise<void>;
}

const MapVersionsContext = createContext<MapVersionsData | null>(null);

export function MapVersionsProvider({
  map,
  children,
}: {
  map: MapSummary | null;
  children: ReactNode;
}) {
  const [versions, setVersions] = useState<MapVersion[]>([]);
  const [error, setError] = useState<string | null>(null);

  const reloadVersions = useCallback(async () => {
    if (!map) return;
    setError(null);
    try {
      setVersions(await apiJson<MapVersion[]>(`/maps/${map.uuid}/versions`));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }, [map]);

  useEffect(() => {
    reloadVersions();
  }, [reloadVersions]);

  const value = useMemo<MapVersionsData>(
    () => ({ versions, error, reloadVersions }),
    [versions, error, reloadVersions],
  );

  return <MapVersionsContext.Provider value={value}>{children}</MapVersionsContext.Provider>;
}

export function useMapVersions(): MapVersionsData {
  const ctx = useContext(MapVersionsContext);
  if (!ctx) throw new Error('useMapVersions must be used within MapVersionsProvider');
  return ctx;
}
