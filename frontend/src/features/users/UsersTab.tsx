import { useState, type FormEvent } from "react";
import { apiFetch, apiPostJSON } from "../../api/client";
import type { User } from "../../api/types";
import { useAdminData } from "../AdminDataContext";
import { useAuth } from "../../auth/AuthContext";
import ErrorBanner from "../../components/ErrorBanner";
import { fmtDate } from "../../lib/format";
import ApiKeysModal from "./ApiKeysModal";

interface UserPermState {
  canCreate: boolean;
  canEdit: boolean;
  canDelete: boolean;
  canEditGeoObjects: boolean;
  canDeleteGeoObjects: boolean;
  canViewAll: boolean;
  isAdmin: boolean;
}

const defaultCreatePerms: UserPermState = {
  canCreate: true,
  canEdit: true,
  canDelete: true,
  canEditGeoObjects: true,
  canDeleteGeoObjects: true,
  canViewAll: false,
  isAdmin: false,
};

function UserRow({ u, self, onReload, onOpenApiKeys }: { u: User; self: boolean; onReload: () => void; onOpenApiKeys: () => void }) {
  const [perms, setPerms] = useState<UserPermState>(u);
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);

  const save = async () => {
    setError(null);
    try {
      await apiFetch(`/users/${encodeURIComponent(u.username)}`, {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ ...perms, password }),
      });
      await onReload();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  const remove = async () => {
    if (!confirm(`Delete user "${u.username}"?`)) return;
    setError(null);
    try {
      await apiFetch(`/users/${encodeURIComponent(u.username)}`, { method: "DELETE" });
      await onReload();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  const cb = (key: keyof UserPermState) => (
    <input type="checkbox" checked={perms[key]} onChange={(e) => setPerms((p) => ({ ...p, [key]: e.target.checked }))} />
  );

  return (
    <tr>
      <td>
        {u.username}
        {self ? " (you)" : ""}
        <ErrorBanner message={error} />
      </td>
      <td className="checkbox-cell">{cb("canCreate")}</td>
      <td className="checkbox-cell">{cb("canEdit")}</td>
      <td className="checkbox-cell">{cb("canDelete")}</td>
      <td className="checkbox-cell">{cb("canEditGeoObjects")}</td>
      <td className="checkbox-cell">{cb("canDeleteGeoObjects")}</td>
      <td className="checkbox-cell">{cb("canViewAll")}</td>
      <td className="checkbox-cell">{cb("isAdmin")}</td>
      <td>
        <input
          type="password"
          className="inline"
          placeholder="leave blank to keep"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
        />
      </td>
      <td>{fmtDate(u.createdAt)}</td>
      <td>
        <div className="actions">
          <button type="button" className="secondary" onClick={save}>
            Save
          </button>
          <button type="button" className="secondary" onClick={onOpenApiKeys}>
            API keys
          </button>
          <button type="button" className="danger" disabled={self} title={self ? "You can't delete your own account" : undefined} onClick={remove}>
            Delete
          </button>
        </div>
      </td>
    </tr>
  );
}

export default function UsersTab() {
  const { users, reloadUsers } = useAdminData();
  const { username: currentUsername } = useAuth();
  const [error, setError] = useState<string | null>(null);
  const [apiKeysUser, setApiKeysUser] = useState<string | null>(null);
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [createPerms, setCreatePerms] = useState<UserPermState>(defaultCreatePerms);

  const createUser = async (e: FormEvent) => {
    e.preventDefault();
    setError(null);
    try {
      await apiPostJSON("/users", { username, password, ...createPerms });
      setUsername("");
      setPassword("");
      setCreatePerms(defaultCreatePerms);
      await reloadUsers();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  const cb = (key: keyof UserPermState, label: string) => (
    <div className="field">
      <label>
        <input
          type="checkbox"
          checked={createPerms[key]}
          onChange={(e) => setCreatePerms((p) => ({ ...p, [key]: e.target.checked }))}
        />{" "}
        {label}
      </label>
    </div>
  );

  return (
    <div>
      <div className="card">
        <h2>Create user</h2>
        <form className="row" onSubmit={createUser}>
          <div className="field">
            <label htmlFor="cu-username">Username</label>
            <input id="cu-username" required value={username} onChange={(e) => setUsername(e.target.value)} />
          </div>
          <div className="field">
            <label htmlFor="cu-password">Password</label>
            <input id="cu-password" type="password" required value={password} onChange={(e) => setPassword(e.target.value)} />
          </div>
          {cb("canCreate", "Can create")}
          {cb("canEdit", "Can edit")}
          {cb("canDelete", "Can delete")}
          {cb("canEditGeoObjects", "Can edit geo objects")}
          {cb("canDeleteGeoObjects", "Can delete geo objects")}
          {cb("canViewAll", "Can view all maps")}
          {cb("isAdmin", "Admin")}
          <button type="submit">Create</button>
        </form>
      </div>

      <div className="card">
        <ErrorBanner message={error} />
        <table>
          <thead>
            <tr>
              <th>Username</th>
              <th className="checkbox-cell">Create</th>
              <th className="checkbox-cell">Edit</th>
              <th className="checkbox-cell">Delete</th>
              <th className="checkbox-cell">Geo edit</th>
              <th className="checkbox-cell">Geo delete</th>
              <th className="checkbox-cell">View all</th>
              <th className="checkbox-cell">Admin</th>
              <th>New password</th>
              <th>Created</th>
              <th>Actions</th>
            </tr>
          </thead>
          <tbody>
            {users.map((u) => (
              <UserRow
                key={u.username}
                u={u}
                self={u.username === currentUsername}
                onReload={reloadUsers}
                onOpenApiKeys={() => setApiKeysUser(u.username)}
              />
            ))}
          </tbody>
        </table>
      </div>

      <ApiKeysModal username={apiKeysUser} onClose={() => setApiKeysUser(null)} />
    </div>
  );
}
