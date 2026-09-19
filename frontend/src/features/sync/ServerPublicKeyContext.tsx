import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from 'react';
import { apiJson } from '../../api/client';

interface ServerPublicKeyData {
  publicKeyPem: string;
  error: string | null;
}

const ServerPublicKeyContext = createContext<ServerPublicKeyData | null>(null);

export function ServerPublicKeyProvider({ children }: { children: ReactNode }) {
  const [publicKeyPem, setPublicKeyPem] = useState('');
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    apiJson<{ publicKeyPem: string }>('/server/public-key')
      .then((data) => setPublicKeyPem(data.publicKeyPem))
      .catch((err) => setError(err instanceof Error ? err.message : String(err)));
  }, []);

  const value = useMemo<ServerPublicKeyData>(
    () => ({ publicKeyPem, error }),
    [publicKeyPem, error],
  );

  return (
    <ServerPublicKeyContext.Provider value={value}>{children}</ServerPublicKeyContext.Provider>
  );
}

export function useServerPublicKey(): ServerPublicKeyData {
  const ctx = useContext(ServerPublicKeyContext);
  if (!ctx) throw new Error('useServerPublicKey must be used within ServerPublicKeyProvider');
  return ctx;
}
