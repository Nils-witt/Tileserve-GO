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
import type { User } from '../../api/types';

interface UsersData {
  users: User[];
  reloadUsers: () => Promise<void>;
}

const UsersContext = createContext<UsersData | null>(null);

// GET /users is open to every authenticated user (so a map owner can pick a
// username when granting a per-map permission), not just admins.
export function UsersProvider({ children }: { children: ReactNode }) {
  const [users, setUsers] = useState<User[]>([]);

  const reloadUsers = useCallback(async () => {
    setUsers(await apiJson<User[]>('/users'));
  }, []);

  useEffect(() => {
    reloadUsers().catch(() => {});
  }, [reloadUsers]);

  const value = useMemo<UsersData>(() => ({ users, reloadUsers }), [users, reloadUsers]);

  return <UsersContext.Provider value={value}>{children}</UsersContext.Provider>;
}

export function useUsers(): UsersData {
  const ctx = useContext(UsersContext);
  if (!ctx) throw new Error('useUsers must be used within UsersProvider');
  return ctx;
}
