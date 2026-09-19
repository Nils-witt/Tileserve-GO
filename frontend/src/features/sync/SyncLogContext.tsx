import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from 'react';
import { useApi } from '../../api/ApiContext';
import type { SyncLogEntry, SyncRemote } from '../../api/types';

interface SyncLogData {
  entries: SyncLogEntry[];
  error: string | null;
  reloadLog: () => Promise<void>;
}

const SyncLogContext = createContext<SyncLogData | null>(null);

export function SyncLogProvider({
  remote,
  children,
}: {
  remote: SyncRemote | null;
  children: ReactNode;
}) {
  const api = useApi();
  const [entries, setEntries] = useState<SyncLogEntry[]>([]);
  const [error, setError] = useState<string | null>(null);

  const reloadLog = useCallback(async () => {
    if (!remote) return;
    setError(null);
    try {
      setEntries(await api.listSyncRemoteLogs(remote.id));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }, [api, remote]);

  useEffect(() => {
    reloadLog();
  }, [reloadLog]);

  const value = useMemo<SyncLogData>(
    () => ({ entries, error, reloadLog }),
    [entries, error, reloadLog],
  );

  return <SyncLogContext.Provider value={value}>{children}</SyncLogContext.Provider>;
}

export function useSyncLog(): SyncLogData {
  const ctx = useContext(SyncLogContext);
  if (!ctx) throw new Error('useSyncLog must be used within SyncLogProvider');
  return ctx;
}
