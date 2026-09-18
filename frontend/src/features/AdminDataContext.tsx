import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from 'react';
import { apiJson } from '../api/client';
import type { CurrentPermissions, Group, MapSummary, User } from '../api/types';

// AdminDataContext holds the three lists (maps/users/groups) that most tabs
// and modals need — either to render their own table or just to resolve a
// uuid to a display name (mapName/groupName), matching the previous UI's
// module-level allMaps/allUsers/allGroups arrays loaded once on sign-in.
interface AdminData {
  isAdmin: boolean;
  maps: MapSummary[];
  users: User[];
  groups: Group[];
  reloadMaps: () => Promise<void>;
  reloadUsers: () => Promise<void>;
  reloadGroups: () => Promise<void>;
  mapName: (uuid: string) => string;
  groupName: (id: string) => string;
}

const AdminDataContext = createContext<AdminData | null>(null);

export function AdminDataProvider({ children }: { children: ReactNode }) {
  const [isAdmin, setIsAdmin] = useState(false);
  const [maps, setMaps] = useState<MapSummary[]>([]);
  const [users, setUsers] = useState<User[]>([]);
  const [groups, setGroups] = useState<Group[]>([]);

  const reloadMaps = useCallback(async () => {
    setMaps(await apiJson<MapSummary[]>('/maps'));
  }, []);
  const reloadUsers = useCallback(async () => {
    setUsers(await apiJson<User[]>('/users'));
  }, []);
  const reloadGroups = useCallback(async () => {
    setGroups(await apiJson<Group[]>('/groups'));
  }, []);

  useEffect(() => {
    // GET /permissions/me returns the caller's own effective (group-merged)
    // permissions — the same isAdmin value every admin-only route on the
    // server actually checks, unlike a user's own /users row, which only
    // carries their personal isAdmin flag and misses admin rights granted
    // through a group they belong to.
    apiJson<CurrentPermissions>('/permissions/me')
      .then((perms) => setIsAdmin(!!perms.isAdmin))
      .catch(() => setIsAdmin(false));

    reloadMaps().catch(() => {});
    // GET /users and GET /groups are open to every authenticated user (so a
    // map owner can pick a username/group when granting a per-map
    // permission), not just admins.
    reloadUsers().catch(() => {});
    reloadGroups().catch(() => {});
  }, [reloadMaps, reloadUsers, reloadGroups]);

  const mapsByUuid = useMemo(() => new Map(maps.map((m) => [m.uuid, m])), [maps]);
  const groupsById = useMemo(() => new Map(groups.map((g) => [g.id, g])), [groups]);

  const mapName = useCallback((uuid: string) => mapsByUuid.get(uuid)?.name ?? uuid, [mapsByUuid]);
  const groupName = useCallback((id: string) => groupsById.get(id)?.name ?? id, [groupsById]);

  const value = useMemo<AdminData>(
    () => ({
      isAdmin,
      maps,
      users,
      groups,
      reloadMaps,
      reloadUsers,
      reloadGroups,
      mapName,
      groupName,
    }),
    [isAdmin, maps, users, groups, reloadMaps, reloadUsers, reloadGroups, mapName, groupName],
  );

  return <AdminDataContext.Provider value={value}>{children}</AdminDataContext.Provider>;
}

export function useAdminData(): AdminData {
  const ctx = useContext(AdminDataContext);
  if (!ctx) throw new Error('useAdminData must be used within AdminDataProvider');
  return ctx;
}
