import { useCallback, useEffect, useState } from "react";
import { apiFetch, apiJson } from "../../api/client";
import type { MapGroupPermission, MapPermission, MapSummary } from "../../api/types";
import { useAdminData } from "../AdminDataContext";
import Modal from "../../components/Modal";
import ErrorBanner from "../../components/ErrorBanner";

interface GrantFormState {
  canView: boolean;
  canEdit: boolean;
  canDelete: boolean;
  canEditGeoObjects: boolean;
  canDeleteGeoObjects: boolean;
}

const defaultGrant: GrantFormState = {
  canView: true,
  canEdit: false,
  canDelete: false,
  canEditGeoObjects: false,
  canDeleteGeoObjects: false,
};

function GrantCheckboxes({
  value,
  onChange,
}: {
  value: GrantFormState;
  onChange: (patch: Partial<GrantFormState>) => void;
}) {
  return (
    <>
      <td className="checkbox-cell">
        <input type="checkbox" checked={value.canView} onChange={(e) => onChange({ canView: e.target.checked })} />
      </td>
      <td className="checkbox-cell">
        <input type="checkbox" checked={value.canEdit} onChange={(e) => onChange({ canEdit: e.target.checked })} />
      </td>
      <td className="checkbox-cell">
        <input type="checkbox" checked={value.canDelete} onChange={(e) => onChange({ canDelete: e.target.checked })} />
      </td>
      <td className="checkbox-cell">
        <input
          type="checkbox"
          checked={value.canEditGeoObjects}
          onChange={(e) => onChange({ canEditGeoObjects: e.target.checked })}
        />
      </td>
      <td className="checkbox-cell">
        <input
          type="checkbox"
          checked={value.canDeleteGeoObjects}
          onChange={(e) => onChange({ canDeleteGeoObjects: e.target.checked })}
        />
      </td>
    </>
  );
}

function UserPermRow({ grant, onSave, onRevoke }: { grant: MapPermission; onSave: (v: GrantFormState) => void; onRevoke: () => void }) {
  const [value, setValue] = useState<GrantFormState>(grant);
  return (
    <tr>
      <td>{grant.username}</td>
      <GrantCheckboxes value={value} onChange={(patch) => setValue((v) => ({ ...v, ...patch }))} />
      <td>
        <div className="actions">
          <button type="button" className="secondary" onClick={() => onSave(value)}>
            Save
          </button>
          <button type="button" className="danger" onClick={onRevoke}>
            Revoke
          </button>
        </div>
      </td>
    </tr>
  );
}

function GroupPermRow({
  grant,
  name,
  onSave,
  onRevoke,
}: {
  grant: MapGroupPermission;
  name: string;
  onSave: (v: GrantFormState) => void;
  onRevoke: () => void;
}) {
  const [value, setValue] = useState<GrantFormState>(grant);
  return (
    <tr>
      <td>{name}</td>
      <GrantCheckboxes value={value} onChange={(patch) => setValue((v) => ({ ...v, ...patch }))} />
      <td>
        <div className="actions">
          <button type="button" className="secondary" onClick={() => onSave(value)}>
            Save
          </button>
          <button type="button" className="danger" onClick={onRevoke}>
            Revoke
          </button>
        </div>
      </td>
    </tr>
  );
}

export default function PermissionsModal({ map, onClose }: { map: MapSummary | null; onClose: () => void }) {
  const { users, groups, groupName } = useAdminData();
  const [grants, setGrants] = useState<MapPermission[]>([]);
  const [groupGrants, setGroupGrants] = useState<MapGroupPermission[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [groupError, setGroupError] = useState<string | null>(null);
  const [addUser, setAddUser] = useState("");
  const [addGrant, setAddGrant] = useState<GrantFormState>(defaultGrant);
  const [addGroup, setAddGroup] = useState("");
  const [addGroupGrant, setAddGroupGrant] = useState<GrantFormState>(defaultGrant);

  const loadPermissions = useCallback(async () => {
    if (!map) return;
    setError(null);
    try {
      setGrants(await apiJson<MapPermission[]>(`/maps/${map.uuid}/permissions`));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }, [map]);

  const loadGroupPermissions = useCallback(async () => {
    if (!map) return;
    setGroupError(null);
    try {
      setGroupGrants(await apiJson<MapGroupPermission[]>(`/maps/${map.uuid}/group-permissions`));
    } catch (err) {
      setGroupError(err instanceof Error ? err.message : String(err));
    }
  }, [map]);

  useEffect(() => {
    if (!map) return;
    setAddUser("");
    setAddGroup("");
    setAddGrant(defaultGrant);
    setAddGroupGrant(defaultGrant);
    loadPermissions();
    loadGroupPermissions();
  }, [map, loadPermissions, loadGroupPermissions]);

  const grantUser = async (username: string, grant: GrantFormState) => {
    if (!map) return;
    setError(null);
    try {
      await apiFetch(`/maps/${map.uuid}/permissions/${encodeURIComponent(username)}`, {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(grant),
      });
      await loadPermissions();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  const revokeUser = async (username: string) => {
    if (!map) return;
    setError(null);
    try {
      await apiFetch(`/maps/${map.uuid}/permissions/${encodeURIComponent(username)}`, { method: "DELETE" });
      await loadPermissions();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  const grantGroup = async (groupId: string, grant: GrantFormState) => {
    if (!map) return;
    setGroupError(null);
    try {
      await apiFetch(`/maps/${map.uuid}/group-permissions/${encodeURIComponent(groupId)}`, {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(grant),
      });
      await loadGroupPermissions();
    } catch (err) {
      setGroupError(err instanceof Error ? err.message : String(err));
    }
  };

  const revokeGroup = async (groupId: string) => {
    if (!map) return;
    setGroupError(null);
    try {
      await apiFetch(`/maps/${map.uuid}/group-permissions/${encodeURIComponent(groupId)}`, { method: "DELETE" });
      await loadGroupPermissions();
    } catch (err) {
      setGroupError(err instanceof Error ? err.message : String(err));
    }
  };

  return (
    <Modal open={!!map} title={map ? `Permissions — ${map.name}` : "Permissions"} onClose={onClose}>
      <ErrorBanner message={error} />
      <table>
        <thead>
          <tr>
            <th>User</th>
            <th className="checkbox-cell">View</th>
            <th className="checkbox-cell">Edit</th>
            <th className="checkbox-cell">Delete</th>
            <th className="checkbox-cell">Geo edit</th>
            <th className="checkbox-cell">Geo delete</th>
            <th>Actions</th>
          </tr>
        </thead>
        <tbody>
          {grants.map((g) => (
            <UserPermRow key={g.username} grant={g} onSave={(v) => grantUser(g.username, v)} onRevoke={() => revokeUser(g.username)} />
          ))}
        </tbody>
      </table>
      {grants.length === 0 && <p className="muted">No per-map grants yet.</p>}
      <div className="row" style={{ marginTop: "1rem" }}>
        <div className="field">
          <label htmlFor="perm-add-user">Add user</label>
          <select id="perm-add-user" value={addUser} onChange={(e) => setAddUser(e.target.value)}>
            <option value="" />
            {users.map((u) => (
              <option key={u.username} value={u.username}>
                {u.username}
              </option>
            ))}
          </select>
        </div>
        <div className="field">
          <label>
            <input type="checkbox" checked={addGrant.canView} onChange={(e) => setAddGrant((v) => ({ ...v, canView: e.target.checked }))} />{" "}
            Can view
          </label>
        </div>
        <div className="field">
          <label>
            <input type="checkbox" checked={addGrant.canEdit} onChange={(e) => setAddGrant((v) => ({ ...v, canEdit: e.target.checked }))} />{" "}
            Can edit
          </label>
        </div>
        <div className="field">
          <label>
            <input
              type="checkbox"
              checked={addGrant.canDelete}
              onChange={(e) => setAddGrant((v) => ({ ...v, canDelete: e.target.checked }))}
            />{" "}
            Can delete
          </label>
        </div>
        <div className="field">
          <label>
            <input
              type="checkbox"
              checked={addGrant.canEditGeoObjects}
              onChange={(e) => setAddGrant((v) => ({ ...v, canEditGeoObjects: e.target.checked }))}
            />{" "}
            Can edit geo objects
          </label>
        </div>
        <div className="field">
          <label>
            <input
              type="checkbox"
              checked={addGrant.canDeleteGeoObjects}
              onChange={(e) => setAddGrant((v) => ({ ...v, canDeleteGeoObjects: e.target.checked }))}
            />{" "}
            Can delete geo objects
          </label>
        </div>
        <button type="button" disabled={!addUser} onClick={() => grantUser(addUser, addGrant)}>
          Grant
        </button>
      </div>

      <h2 style={{ marginTop: "1.25rem" }}>Group permissions</h2>
      <ErrorBanner message={groupError} />
      <table>
        <thead>
          <tr>
            <th>Group</th>
            <th className="checkbox-cell">View</th>
            <th className="checkbox-cell">Edit</th>
            <th className="checkbox-cell">Delete</th>
            <th className="checkbox-cell">Geo edit</th>
            <th className="checkbox-cell">Geo delete</th>
            <th>Actions</th>
          </tr>
        </thead>
        <tbody>
          {groupGrants.map((g) => (
            <GroupPermRow
              key={g.groupId}
              grant={g}
              name={groupName(g.groupId)}
              onSave={(v) => grantGroup(g.groupId, v)}
              onRevoke={() => revokeGroup(g.groupId)}
            />
          ))}
        </tbody>
      </table>
      {groupGrants.length === 0 && <p className="muted">No per-group grants yet.</p>}
      <div className="row" style={{ marginTop: "1rem" }}>
        <div className="field">
          <label htmlFor="group-perm-add-group">Add group</label>
          <select id="group-perm-add-group" value={addGroup} onChange={(e) => setAddGroup(e.target.value)}>
            <option value="" />
            {groups.map((g) => (
              <option key={g.id} value={g.id}>
                {g.name}
              </option>
            ))}
          </select>
        </div>
        <div className="field">
          <label>
            <input
              type="checkbox"
              checked={addGroupGrant.canView}
              onChange={(e) => setAddGroupGrant((v) => ({ ...v, canView: e.target.checked }))}
            />{" "}
            Can view
          </label>
        </div>
        <div className="field">
          <label>
            <input
              type="checkbox"
              checked={addGroupGrant.canEdit}
              onChange={(e) => setAddGroupGrant((v) => ({ ...v, canEdit: e.target.checked }))}
            />{" "}
            Can edit
          </label>
        </div>
        <div className="field">
          <label>
            <input
              type="checkbox"
              checked={addGroupGrant.canDelete}
              onChange={(e) => setAddGroupGrant((v) => ({ ...v, canDelete: e.target.checked }))}
            />{" "}
            Can delete
          </label>
        </div>
        <div className="field">
          <label>
            <input
              type="checkbox"
              checked={addGroupGrant.canEditGeoObjects}
              onChange={(e) => setAddGroupGrant((v) => ({ ...v, canEditGeoObjects: e.target.checked }))}
            />{" "}
            Can edit geo objects
          </label>
        </div>
        <div className="field">
          <label>
            <input
              type="checkbox"
              checked={addGroupGrant.canDeleteGeoObjects}
              onChange={(e) => setAddGroupGrant((v) => ({ ...v, canDeleteGeoObjects: e.target.checked }))}
            />{" "}
            Can delete geo objects
          </label>
        </div>
        <button type="button" disabled={!addGroup} onClick={() => grantGroup(addGroup, addGroupGrant)}>
          Grant
        </button>
      </div>
    </Modal>
  );
}
