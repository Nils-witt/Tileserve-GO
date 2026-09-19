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
import type { MapSummary } from '../../api/types';

interface MapsData {
  maps: MapSummary[];
  reloadMaps: () => Promise<void>;
  mapName: (uuid: string) => string;
}

const MapsContext = createContext<MapsData | null>(null);

export function MapsProvider({ children }: { children: ReactNode }) {
  const [maps, setMaps] = useState<MapSummary[]>([]);

  const reloadMaps = useCallback(async () => {
    setMaps(await apiJson<MapSummary[]>('/maps'));
  }, []);

  useEffect(() => {
    reloadMaps().catch(() => {});
  }, [reloadMaps]);

  const mapsByUuid = useMemo(() => new Map(maps.map((m) => [m.uuid, m])), [maps]);
  const mapName = useCallback((uuid: string) => mapsByUuid.get(uuid)?.name ?? uuid, [mapsByUuid]);

  const value = useMemo<MapsData>(
    () => ({ maps, reloadMaps, mapName }),
    [maps, reloadMaps, mapName],
  );

  return <MapsContext.Provider value={value}>{children}</MapsContext.Provider>;
}

export function useMaps(): MapsData {
  const ctx = useContext(MapsContext);
  if (!ctx) throw new Error('useMaps must be used within MapsProvider');
  return ctx;
}
