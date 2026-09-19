import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from 'react';
import { apiJson } from '../api/client';
import type { CurrentPermissions } from '../api/types';

interface CurrentPermissionsData {
  isAdmin: boolean;
}

const CurrentPermissionsContext = createContext<CurrentPermissionsData | null>(null);

export function CurrentPermissionsProvider({ children }: { children: ReactNode }) {

  const [isAdmin, setIsAdmin] = useState(false);

  useEffect(() => {
    // GET /permissions/me returns the caller's own effective (group-merged)
    // permissions — the same isAdmin value every admin-only route on the
    // server actually checks, unlike a user's own /users row, which only
    // carries their personal isAdmin flag and misses admin rights granted
    // through a group they belong to.
    apiJson<CurrentPermissions>('/permissions/me')
      .then((perms) => setIsAdmin(!!perms.isAdmin))
      .catch(() => setIsAdmin(false));
  }, []);

  const value = useMemo<CurrentPermissionsData>(() => ({ isAdmin }), [isAdmin]);

  return (
    <CurrentPermissionsContext.Provider value={value}>
      {children}
    </CurrentPermissionsContext.Provider>
  );
}

export function useCurrentPermissions(): CurrentPermissionsData {
  const ctx = useContext(CurrentPermissionsContext);
  if (!ctx) throw new Error('useCurrentPermissions must be used within CurrentPermissionsProvider');
  return ctx;
}
