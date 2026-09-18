import { useCallback, useEffect, useState } from 'react';
import {
  Box,
  Button,
  Checkbox,
  FormControl,
  FormControlLabel,
  InputLabel,
  MenuItem,
  Select,
  Stack,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  Typography,
} from '@mui/material';
import { apiFetch, apiJson } from '../../api/client';
import type { MapGroupPermission, MapPermission, MapSummary } from '../../api/types';
import { useAdminData } from '../AdminDataContext';
import ErrorBanner from '../../components/ErrorBanner';

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
      <TableCell align="center" padding="checkbox">
        <Checkbox
          checked={value.canView}
          onChange={(e) => onChange({ canView: e.target.checked })}
        />
      </TableCell>
      <TableCell align="center" padding="checkbox">
        <Checkbox
          checked={value.canEdit}
          onChange={(e) => onChange({ canEdit: e.target.checked })}
        />
      </TableCell>
      <TableCell align="center" padding="checkbox">
        <Checkbox
          checked={value.canDelete}
          onChange={(e) => onChange({ canDelete: e.target.checked })}
        />
      </TableCell>
      <TableCell align="center" padding="checkbox">
        <Checkbox
          checked={value.canEditGeoObjects}
          onChange={(e) => onChange({ canEditGeoObjects: e.target.checked })}
        />
      </TableCell>
      <TableCell align="center" padding="checkbox">
        <Checkbox
          checked={value.canDeleteGeoObjects}
          onChange={(e) => onChange({ canDeleteGeoObjects: e.target.checked })}
        />
      </TableCell>
    </>
  );
}

function UserPermRow({
  grant,
  onSave,
  onRevoke,
}: {
  grant: MapPermission;
  onSave: (v: GrantFormState) => void;
  onRevoke: () => void;
}) {
  const [value, setValue] = useState<GrantFormState>(grant);
  return (
    <TableRow>
      <TableCell>{grant.username}</TableCell>
      <GrantCheckboxes value={value} onChange={(patch) => setValue((v) => ({ ...v, ...patch }))} />
      <TableCell>
        <Stack direction="row" spacing={1}>
          <Button size="small" onClick={() => onSave(value)}>
            Save
          </Button>
          <Button size="small" color="error" onClick={onRevoke}>
            Revoke
          </Button>
        </Stack>
      </TableCell>
    </TableRow>
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
    <TableRow>
      <TableCell>{name}</TableCell>
      <GrantCheckboxes value={value} onChange={(patch) => setValue((v) => ({ ...v, ...patch }))} />
      <TableCell>
        <Stack direction="row" spacing={1}>
          <Button size="small" onClick={() => onSave(value)}>
            Save
          </Button>
          <Button size="small" color="error" onClick={onRevoke}>
            Revoke
          </Button>
        </Stack>
      </TableCell>
    </TableRow>
  );
}

function GrantCheckboxFields({
  value,
  onChange,
}: {
  value: GrantFormState;
  onChange: (patch: Partial<GrantFormState>) => void;
}) {
  return (
    <>
      <FormControlLabel
        control={
          <Checkbox
            checked={value.canView}
            onChange={(e) => onChange({ canView: e.target.checked })}
          />
        }
        label="Can view"
      />
      <FormControlLabel
        control={
          <Checkbox
            checked={value.canEdit}
            onChange={(e) => onChange({ canEdit: e.target.checked })}
          />
        }
        label="Can edit"
      />
      <FormControlLabel
        control={
          <Checkbox
            checked={value.canDelete}
            onChange={(e) => onChange({ canDelete: e.target.checked })}
          />
        }
        label="Can delete"
      />
      <FormControlLabel
        control={
          <Checkbox
            checked={value.canEditGeoObjects}
            onChange={(e) => onChange({ canEditGeoObjects: e.target.checked })}
          />
        }
        label="Can edit geo objects"
      />
      <FormControlLabel
        control={
          <Checkbox
            checked={value.canDeleteGeoObjects}
            onChange={(e) => onChange({ canDeleteGeoObjects: e.target.checked })}
          />
        }
        label="Can delete geo objects"
      />
    </>
  );
}

export default function PermissionsPanel({ map }: { map: MapSummary }) {
  const { users, groups, groupName } = useAdminData();
  const [grants, setGrants] = useState<MapPermission[]>([]);
  const [groupGrants, setGroupGrants] = useState<MapGroupPermission[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [groupError, setGroupError] = useState<string | null>(null);
  const [addUser, setAddUser] = useState('');
  const [addGrant, setAddGrant] = useState<GrantFormState>(defaultGrant);
  const [addGroup, setAddGroup] = useState('');
  const [addGroupGrant, setAddGroupGrant] = useState<GrantFormState>(defaultGrant);

  const loadPermissions = useCallback(async () => {
    setError(null);
    try {
      setGrants(await apiJson<MapPermission[]>(`/maps/${map.uuid}/permissions`));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }, [map]);

  const loadGroupPermissions = useCallback(async () => {
    setGroupError(null);
    try {
      setGroupGrants(await apiJson<MapGroupPermission[]>(`/maps/${map.uuid}/group-permissions`));
    } catch (err) {
      setGroupError(err instanceof Error ? err.message : String(err));
    }
  }, [map]);

  useEffect(() => {
    setAddUser('');
    setAddGroup('');
    setAddGrant(defaultGrant);
    setAddGroupGrant(defaultGrant);
    loadPermissions();
    loadGroupPermissions();
  }, [map, loadPermissions, loadGroupPermissions]);

  const grantUser = async (username: string, grant: GrantFormState) => {
    setError(null);
    try {
      await apiFetch(`/maps/${map.uuid}/permissions/${encodeURIComponent(username)}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(grant),
      });
      await loadPermissions();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  const revokeUser = async (username: string) => {
    setError(null);
    try {
      await apiFetch(`/maps/${map.uuid}/permissions/${encodeURIComponent(username)}`, {
        method: 'DELETE',
      });
      await loadPermissions();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  const grantGroup = async (groupId: string, grant: GrantFormState) => {
    setGroupError(null);
    try {
      await apiFetch(`/maps/${map.uuid}/group-permissions/${encodeURIComponent(groupId)}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(grant),
      });
      await loadGroupPermissions();
    } catch (err) {
      setGroupError(err instanceof Error ? err.message : String(err));
    }
  };

  const revokeGroup = async (groupId: string) => {
    setGroupError(null);
    try {
      await apiFetch(`/maps/${map.uuid}/group-permissions/${encodeURIComponent(groupId)}`, {
        method: 'DELETE',
      });
      await loadGroupPermissions();
    } catch (err) {
      setGroupError(err instanceof Error ? err.message : String(err));
    }
  };

  return (
    <>
      <ErrorBanner message={error} />
      <TableContainer>
        <Table size="small">
          <TableHead>
            <TableRow>
              <TableCell>User</TableCell>
              <TableCell align="center">View</TableCell>
              <TableCell align="center">Edit</TableCell>
              <TableCell align="center">Delete</TableCell>
              <TableCell align="center">Geo edit</TableCell>
              <TableCell align="center">Geo delete</TableCell>
              <TableCell>Actions</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {grants.map((g) => (
              <UserPermRow
                key={g.username}
                grant={g}
                onSave={(v) => grantUser(g.username, v)}
                onRevoke={() => revokeUser(g.username)}
              />
            ))}
          </TableBody>
        </Table>
      </TableContainer>
      {grants.length === 0 && (
        <Typography variant="body2" color="text.secondary" sx={{ mt: 1 }}>
          No per-map grants yet.
        </Typography>
      )}
      <Stack
        direction="row"
        spacing={2}
        useFlexGap
        sx={{ flexWrap: 'wrap', alignItems: 'center', mt: 2 }}
      >
        <FormControl size="small" sx={{ minWidth: 180 }}>
          <InputLabel id="perm-add-user-label">Add user</InputLabel>
          <Select
            labelId="perm-add-user-label"
            label="Add user"
            value={addUser}
            onChange={(e) => setAddUser(e.target.value)}
          >
            <MenuItem value="">
              <em>None</em>
            </MenuItem>
            {users.map((u) => (
              <MenuItem key={u.username} value={u.username}>
                {u.username}
              </MenuItem>
            ))}
          </Select>
        </FormControl>
        <GrantCheckboxFields
          value={addGrant}
          onChange={(patch) => setAddGrant((v) => ({ ...v, ...patch }))}
        />
        <Button
          variant="contained"
          disabled={!addUser}
          onClick={() => grantUser(addUser, addGrant)}
        >
          Grant
        </Button>
      </Stack>

      <Typography variant="h6" component="h2" sx={{ mt: 3, mb: 1 }}>
        Group permissions
      </Typography>
      <ErrorBanner message={groupError} />
      <TableContainer>
        <Table size="small">
          <TableHead>
            <TableRow>
              <TableCell>Group</TableCell>
              <TableCell align="center">View</TableCell>
              <TableCell align="center">Edit</TableCell>
              <TableCell align="center">Delete</TableCell>
              <TableCell align="center">Geo edit</TableCell>
              <TableCell align="center">Geo delete</TableCell>
              <TableCell>Actions</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {groupGrants.map((g) => (
              <GroupPermRow
                key={g.groupId}
                grant={g}
                name={groupName(g.groupId)}
                onSave={(v) => grantGroup(g.groupId, v)}
                onRevoke={() => revokeGroup(g.groupId)}
              />
            ))}
          </TableBody>
        </Table>
      </TableContainer>
      {groupGrants.length === 0 && (
        <Typography variant="body2" color="text.secondary" sx={{ mt: 1 }}>
          No per-group grants yet.
        </Typography>
      )}
      <Stack
        direction="row"
        spacing={2}
        useFlexGap
        sx={{ flexWrap: 'wrap', alignItems: 'center', mt: 2 }}
      >
        <FormControl size="small" sx={{ minWidth: 180 }}>
          <InputLabel id="group-perm-add-group-label">Add group</InputLabel>
          <Select
            labelId="group-perm-add-group-label"
            label="Add group"
            value={addGroup}
            onChange={(e) => setAddGroup(e.target.value)}
          >
            <MenuItem value="">
              <em>None</em>
            </MenuItem>
            {groups.map((g) => (
              <MenuItem key={g.id} value={g.id}>
                {g.name}
              </MenuItem>
            ))}
          </Select>
        </FormControl>
        <GrantCheckboxFields
          value={addGroupGrant}
          onChange={(patch) => setAddGroupGrant((v) => ({ ...v, ...patch }))}
        />
        <Button
          variant="contained"
          disabled={!addGroup}
          onClick={() => grantGroup(addGroup, addGroupGrant)}
        >
          Grant
        </Button>
      </Stack>
      <Box sx={{ height: 4 }} />
    </>
  );
}
