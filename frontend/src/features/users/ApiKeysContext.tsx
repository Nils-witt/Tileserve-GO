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
import type { ApiKey } from '../../api/types';

interface ApiKeysData {
  keys: ApiKey[];
  error: string | null;
  reloadKeys: () => Promise<void>;
}

const ApiKeysContext = createContext<ApiKeysData | null>(null);

export function ApiKeysProvider({
  username,
  children,
}: {
  username: string | null;
  children: ReactNode;
}) {
  const api = useApi();
  const [keys, setKeys] = useState<ApiKey[]>([]);
  const [error, setError] = useState<string | null>(null);

  const reloadKeys = useCallback(async () => {
    if (!username) return;
    setError(null);
    try {
      setKeys(await api.listApiKeys(username));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }, [api, username]);

  useEffect(() => {
    reloadKeys();
  }, [reloadKeys]);

  const value = useMemo<ApiKeysData>(
    () => ({ keys, error, reloadKeys }),
    [keys, error, reloadKeys],
  );

  return <ApiKeysContext.Provider value={value}>{children}</ApiKeysContext.Provider>;
}

export function useApiKeys(): ApiKeysData {
  const ctx = useContext(ApiKeysContext);
  if (!ctx) throw new Error('useApiKeys must be used within ApiKeysProvider');
  return ctx;
}
