import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from 'react';
import { useApi } from '../../api/ApiContext';
import type { RemoteMap, SyncRemote } from '../../api/types';

interface SyncRemoteMapsData {
  remoteMaps: RemoteMap[];
  selectedMapUuids: string[];
  error: string | null;
}

const SyncRemoteMapsContext = createContext<SyncRemoteMapsData | null>(null);

export function SyncRemoteMapsProvider({
  remote,
  children,
}: {
  remote: SyncRemote | null;
  children: ReactNode;
}) {
  const api = useApi();
  const [remoteMaps, setRemoteMaps] = useState<RemoteMap[]>([]);
  const [selectedMapUuids, setSelectedMapUuids] = useState<string[]>([]);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!remote) return;
    setError(null);
    (async () => {
      try {
        const [maps, selectedIds] = await Promise.all([
          api.listRemoteMaps(remote.id),
          api.listSelectedRemoteMaps(remote.id),
        ]);
        setRemoteMaps(maps);
        setSelectedMapUuids(selectedIds);
      } catch (err) {
        setError(err instanceof Error ? err.message : String(err));
      }
    })();
  }, [api, remote]);

  const value = useMemo<SyncRemoteMapsData>(
    () => ({ remoteMaps, selectedMapUuids, error }),
    [remoteMaps, selectedMapUuids, error],
  );

  return <SyncRemoteMapsContext.Provider value={value}>{children}</SyncRemoteMapsContext.Provider>;
}

export function useSyncRemoteMaps(): SyncRemoteMapsData {
  const ctx = useContext(SyncRemoteMapsContext);
  if (!ctx) throw new Error('useSyncRemoteMaps must be used within SyncRemoteMapsProvider');
  return ctx;
}
