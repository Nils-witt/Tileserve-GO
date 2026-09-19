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
import type { ApiKey, ApiKeyScope } from '../../api/types';

interface ApiKeyScopesData {
  scopes: ApiKeyScope[];
  error: string | null;
  reloadScopes: () => Promise<void>;
}

const ApiKeyScopesContext = createContext<ApiKeyScopesData | null>(null);

export function ApiKeyScopesProvider({
  username,
  apiKey,
  children,
}: {
  username: string | null;
  apiKey: ApiKey | null;
  children: ReactNode;
}) {
  const [scopes, setScopes] = useState<ApiKeyScope[]>([]);
  const [error, setError] = useState<string | null>(null);

  const reloadScopes = useCallback(async () => {
    if (!username || !apiKey) return;
    setError(null);
    try {
      setScopes(
        await apiJson<ApiKeyScope[]>(
          `/users/${encodeURIComponent(username)}/api-keys/${apiKey.id}/scopes`,
        ),
      );
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }, [username, apiKey]);

  useEffect(() => {
    reloadScopes();
  }, [reloadScopes]);

  const value = useMemo<ApiKeyScopesData>(
    () => ({ scopes, error, reloadScopes }),
    [scopes, error, reloadScopes],
  );

  return <ApiKeyScopesContext.Provider value={value}>{children}</ApiKeyScopesContext.Provider>;
}

export function useApiKeyScopes(): ApiKeyScopesData {
  const ctx = useContext(ApiKeyScopesContext);
  if (!ctx) throw new Error('useApiKeyScopes must be used within ApiKeyScopesProvider');
  return ctx;
}
