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
import type { Group } from '../../api/types';

interface GroupsData {
  groups: Group[];
  reloadGroups: () => Promise<void>;
  groupName: (id: string) => string;
}

const GroupsContext = createContext<GroupsData | null>(null);

// GET /groups is open to every authenticated user (so a map owner can pick a
// group when granting a per-map permission), not just admins.
export function GroupsProvider({ children }: { children: ReactNode }) {
  const api = useApi();
  const [groups, setGroups] = useState<Group[]>([]);

  const reloadGroups = useCallback(async () => {
    setGroups(await api.listGroups());
  }, [api]);

  useEffect(() => {
    reloadGroups().catch(() => {});
  }, [reloadGroups]);

  const groupsById = useMemo(() => new Map(groups.map((g) => [g.id, g])), [groups]);
  const groupName = useCallback((id: string) => groupsById.get(id)?.name ?? id, [groupsById]);

  const value = useMemo<GroupsData>(
    () => ({ groups, reloadGroups, groupName }),
    [groups, reloadGroups, groupName],
  );

  return <GroupsContext.Provider value={value}>{children}</GroupsContext.Provider>;
}

export function useGroups(): GroupsData {
  const ctx = useContext(GroupsContext);
  if (!ctx) throw new Error('useGroups must be used within GroupsProvider');
  return ctx;
}
