import { createContext, type ReactNode, useContext, useMemo } from 'react';
import { ApiClient } from './ApiClient';
import { useAuth } from '../auth/AuthContext.tsx';

const ApiContext = createContext<ApiClient | null>(null);

export function ApiProvider({ children }: { children: ReactNode }) {
  const { token, logout } = useAuth();

  const instance = useMemo(() => {
    return new ApiClient({
      token,
      onClearSession: logout,
    });
  }, [token, logout]);

  return <ApiContext.Provider value={instance}>{children}</ApiContext.Provider>;
}

export function useApi(): ApiClient {
  const ctx = useContext(ApiContext);
  if (!ctx) throw new Error('useApi must be used within ApiProvider');
  return ctx;
}
