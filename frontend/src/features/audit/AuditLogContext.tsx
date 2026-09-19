import {
  createContext,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from 'react';
import { api } from '../../api/ApiClient';
import type { AuditLogEntry } from '../../api/types';

const PAGE_SIZE = 100;

export interface AuditLogFilter {
  actor: string;
  action: string;
  entityType: string;
  entityId: string;
}

export const emptyAuditLogFilter: AuditLogFilter = {
  actor: '',
  action: '',
  entityType: '',
  entityId: '',
};

interface AuditLogData {
  entries: AuditLogEntry[];
  hasMore: boolean;
  error: string | null;
  load: (filter: AuditLogFilter, reset: boolean) => Promise<void>;
}

const AuditLogContext = createContext<AuditLogData | null>(null);

export function AuditLogProvider({ children }: { children: ReactNode }) {
  const [entries, setEntries] = useState<AuditLogEntry[]>([]);
  const [hasMore, setHasMore] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const offsetRef = useRef(0);

  const load = async (f: AuditLogFilter, reset: boolean) => {
    setError(null);
    const nextOffset = reset ? 0 : offsetRef.current;
    try {
      const page = await api.listAuditLogs({ ...f, limit: PAGE_SIZE, offset: nextOffset });
      setEntries((prev) => (reset ? page : [...prev, ...page]));
      offsetRef.current = nextOffset + page.length;
      setHasMore(page.length === PAGE_SIZE);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  useEffect(() => {
    load(emptyAuditLogFilter, true);
    // Only load once, on mount — subsequent loads are user-triggered.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const value = useMemo<AuditLogData>(
    () => ({ entries, hasMore, error, load }),
    [entries, hasMore, error],
  );

  return <AuditLogContext.Provider value={value}>{children}</AuditLogContext.Provider>;
}

export function useAuditLog(): AuditLogData {
  const ctx = useContext(AuditLogContext);
  if (!ctx) throw new Error('useAuditLog must be used within AuditLogProvider');
  return ctx;
}
