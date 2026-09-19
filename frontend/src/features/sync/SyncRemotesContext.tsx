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
import type { SyncRemote } from '../../api/types';

interface SyncRemotesData {
  remotes: SyncRemote[];
  error: string | null;
  reloadRemotes: () => Promise<void>;
}

const SyncRemotesContext = createContext<SyncRemotesData | null>(null);

export function SyncRemotesProvider({ children }: { children: ReactNode }) {
  const [remotes, setRemotes] = useState<SyncRemote[]>([]);
  const [error, setError] = useState<string | null>(null);

  const reloadRemotes = useCallback(async () => {
    setError(null);
    try {
      setRemotes(await apiJson<SyncRemote[]>('/sync/remotes'));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }, []);

  useEffect(() => {
    reloadRemotes();
  }, [reloadRemotes]);

  const value = useMemo<SyncRemotesData>(
    () => ({ remotes, error, reloadRemotes }),
    [remotes, error, reloadRemotes],
  );

  return <SyncRemotesContext.Provider value={value}>{children}</SyncRemotesContext.Provider>;
}

export function useSyncRemotes(): SyncRemotesData {
  const ctx = useContext(SyncRemotesContext);
  if (!ctx) throw new Error('useSyncRemotes must be used within SyncRemotesProvider');
  return ctx;
}
