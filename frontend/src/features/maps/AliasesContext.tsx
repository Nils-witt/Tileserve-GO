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
import type { MapAlias, MapSummary } from '../../api/types';

interface AliasesData {
  aliases: MapAlias[];
  error: string | null;
  reloadAliases: () => Promise<void>;
}

const AliasesContext = createContext<AliasesData | null>(null);

export function AliasesProvider({ map, children }: { map: MapSummary; children: ReactNode }) {
  const [aliases, setAliases] = useState<MapAlias[]>([]);
  const [error, setError] = useState<string | null>(null);

  const reloadAliases = useCallback(async () => {
    setError(null);
    try {
      setAliases(await api.listMapAliases(map.uuid));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }, [map]);

  useEffect(() => {
    reloadAliases();
  }, [reloadAliases]);

  const value = useMemo<AliasesData>(
    () => ({ aliases, error, reloadAliases }),
    [aliases, error, reloadAliases],
  );

  return <AliasesContext.Provider value={value}>{children}</AliasesContext.Provider>;
}

export function useAliases(): AliasesData {
  const ctx = useContext(AliasesContext);
  if (!ctx) throw new Error('useAliases must be used within AliasesProvider');
  return ctx;
}
