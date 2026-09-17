import { useState, type FormEvent } from "react";
import { apiFetch, apiPostJSON } from "../../api/client";
import type { Group } from "../../api/types";
import { useAdminData } from "../AdminDataContext";
import ErrorBanner from "../../components/ErrorBanner";
import { fmtDate } from "../../lib/format";

interface GroupPermState {
  canCreate: boolean;
  canEdit: boolean;
  canDelete: boolean;
  canEditGeoObjects: boolean;
  canDeleteGeoObjects: boolean;
  canViewAll: boolean;
  isAdmin: boolean;
}

const defaultPerms: GroupPermState = {
  canCreate: false,
  canEdit: false,
  canDelete: false,
  canEditGeoObjects: false,
  canDeleteGeoObjects: false,
  canViewAll: false,
  isAdmin: false,
};

function GroupRow({ g, onReload }: { g: Group; onReload: () => void }) {
  const [name, setName] = useState(g.name);
  const [ldapGroupDn, setLdapGroupDn] = useState(g.ldapGroupDn ?? "");
  const [oidcGroupClaim, setOidcGroupClaim] = useState(g.oidcGroupClaim ?? "");
  const [perms, setPerms] = useState<GroupPermState>(g);
  const [error, setError] = useState<string | null>(null);

  const save = async () => {
    setError(null);
    try {
      await apiFetch(`/groups/${encodeURIComponent(g.id)}`, {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ name, ldapGroupDn, oidcGroupClaim, ...perms }),
      });
      await onReload();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  const remove = async () => {
    if (!confirm(`Delete group "${g.name}"?`)) return;
    setError(null);
    try {
      await apiFetch(`/groups/${encodeURIComponent(g.id)}`, { method: "DELETE" });
      await onReload();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  const cb = (key: keyof GroupPermState) => (
    <input type="checkbox" checked={perms[key]} onChange={(e) => setPerms((p) => ({ ...p, [key]: e.target.checked }))} />
  );

  return (
    <tr>
      <td>
        <input value={name} onChange={(e) => setName(e.target.value)} />
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
        <input value={ldapGroupDn} placeholder="not linked" onChange={(e) => setLdapGroupDn(e.target.value)} />
      </td>
      <td>
        <input value={oidcGroupClaim} placeholder="not linked" onChange={(e) => setOidcGroupClaim(e.target.value)} />
      </td>
      <td>{fmtDate(g.createdAt)}</td>
      <td>
        <div className="actions">
          <button type="button" className="secondary" onClick={save}>
            Save
          </button>
          <button type="button" className="danger" onClick={remove}>
            Delete
          </button>
        </div>
      </td>
    </tr>
  );
}

export default function GroupsTab() {
  const { groups, reloadGroups } = useAdminData();
  const [error, setError] = useState<string | null>(null);
  const [name, setName] = useState("");
  const [ldapGroupDn, setLdapGroupDn] = useState("");
  const [oidcGroupClaim, setOidcGroupClaim] = useState("");
  const [perms, setPerms] = useState<GroupPermState>(defaultPerms);

  const createGroup = async (e: FormEvent) => {
    e.preventDefault();
    setError(null);
    try {
      await apiPostJSON("/groups", { name, ldapGroupDn, oidcGroupClaim, ...perms });
      setName("");
      setLdapGroupDn("");
      setOidcGroupClaim("");
      setPerms(defaultPerms);
      await reloadGroups();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  const cb = (key: keyof GroupPermState, label: string) => (
    <div className="field">
      <label>
        <input type="checkbox" checked={perms[key]} onChange={(e) => setPerms((p) => ({ ...p, [key]: e.target.checked }))} /> {label}
      </label>
    </div>
  );

  return (
    <div>
      <div className="card">
        <h2>Create group</h2>
        <p className="muted" style={{ marginTop: 0, marginBottom: "0.9rem" }}>
          Membership is never assigned here — it's fully derived from each member's LDAP <code>memberOf</code> or OIDC{" "}
          <code>groups</code> claim on every login. Set "LDAP group DN" and/or "OIDC groups claim value" below to link this group to a
          directory/identity-provider group; leave either blank if this group isn't sourced from that provider.
        </p>
        <form className="row" onSubmit={createGroup}>
          <div className="field">
            <label htmlFor="cg-name">Name</label>
            <input id="cg-name" required value={name} onChange={(e) => setName(e.target.value)} />
          </div>
          <div className="field">
            <label htmlFor="cg-ldap-dn">LDAP group DN (optional)</label>
            <input
              id="cg-ldap-dn"
              placeholder="cn=editors,ou=groups,dc=example,dc=com"
              value={ldapGroupDn}
              onChange={(e) => setLdapGroupDn(e.target.value)}
            />
          </div>
          <div className="field">
            <label htmlFor="cg-oidc-claim">OIDC groups claim value (optional)</label>
            <input id="cg-oidc-claim" placeholder="e.g. editors" value={oidcGroupClaim} onChange={(e) => setOidcGroupClaim(e.target.value)} />
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
              <th>Name</th>
              <th className="checkbox-cell">Create</th>
              <th className="checkbox-cell">Edit</th>
              <th className="checkbox-cell">Delete</th>
              <th className="checkbox-cell">Geo edit</th>
              <th className="checkbox-cell">Geo delete</th>
              <th className="checkbox-cell">View all</th>
              <th className="checkbox-cell">Admin</th>
              <th>LDAP group DN</th>
              <th>OIDC groups claim value</th>
              <th>Created</th>
              <th>Actions</th>
            </tr>
          </thead>
          <tbody>
            {groups.map((g) => (
              <GroupRow key={g.id} g={g} onReload={reloadGroups} />
            ))}
          </tbody>
        </table>
        {groups.length === 0 && <p className="muted">No groups yet.</p>}
        <p className="muted">
          Every member of a group inherits its permission flags (OR'd with their own) and any per-map grants made to the group under a
          map's "Permissions" — see "Group permissions" there.
        </p>
      </div>
    </div>
  );
}
