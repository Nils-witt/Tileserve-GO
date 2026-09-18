import { useState, type FormEvent, type ReactNode } from 'react';
import {
  Box,
  Button,
  Checkbox,
  FormControlLabel,
  Paper,
  Stack,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  TextField,
  Typography,
} from '@mui/material';
import { apiFetch, apiPostJSON } from '../../api/client';
import type { Group } from '../../api/types';
import { useAdminData } from '../AdminDataContext';
import ErrorBanner from '../../components/ErrorBanner';
import { fmtDate } from '../../lib/format';

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
  const [ldapGroupDn, setLdapGroupDn] = useState(g.ldapGroupDn ?? '');
  const [oidcGroupClaim, setOidcGroupClaim] = useState(g.oidcGroupClaim ?? '');
  const [perms, setPerms] = useState<GroupPermState>(g);
  const [error, setError] = useState<string | null>(null);

  const save = async () => {
    setError(null);
    try {
      await apiFetch(`/groups/${encodeURIComponent(g.id)}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
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
      await apiFetch(`/groups/${encodeURIComponent(g.id)}`, { method: 'DELETE' });
      await onReload();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  const cb = (key: keyof GroupPermState) => (
    <Checkbox
      checked={perms[key]}
      onChange={(e) => setPerms((p) => ({ ...p, [key]: e.target.checked }))}
    />
  );

  return (
    <TableRow>
      <TableCell>
        <TextField size="small" value={name} onChange={(e) => setName(e.target.value)} />
        <ErrorBanner message={error} />
      </TableCell>
      <TableCell align="center" padding="checkbox">
        {cb('canCreate')}
      </TableCell>
      <TableCell align="center" padding="checkbox">
        {cb('canEdit')}
      </TableCell>
      <TableCell align="center" padding="checkbox">
        {cb('canDelete')}
      </TableCell>
      <TableCell align="center" padding="checkbox">
        {cb('canEditGeoObjects')}
      </TableCell>
      <TableCell align="center" padding="checkbox">
        {cb('canDeleteGeoObjects')}
      </TableCell>
      <TableCell align="center" padding="checkbox">
        {cb('canViewAll')}
      </TableCell>
      <TableCell align="center" padding="checkbox">
        {cb('isAdmin')}
      </TableCell>
      <TableCell>
        <TextField
          size="small"
          value={ldapGroupDn}
          placeholder="not linked"
          onChange={(e) => setLdapGroupDn(e.target.value)}
        />
      </TableCell>
      <TableCell>
        <TextField
          size="small"
          value={oidcGroupClaim}
          placeholder="not linked"
          onChange={(e) => setOidcGroupClaim(e.target.value)}
        />
      </TableCell>
      <TableCell>{fmtDate(g.createdAt)}</TableCell>
      <TableCell sx={{ minWidth: 140 }}>
        <Stack direction="row" spacing={1}>
          <Button size="small" onClick={save}>
            Save
          </Button>
          <Button size="small" color="error" onClick={remove}>
            Delete
          </Button>
        </Stack>
      </TableCell>
    </TableRow>
  );
}

export default function GroupsTab() {
  const { groups, reloadGroups } = useAdminData();
  const [error, setError] = useState<string | null>(null);
  const [name, setName] = useState('');
  const [ldapGroupDn, setLdapGroupDn] = useState('');
  const [oidcGroupClaim, setOidcGroupClaim] = useState('');
  const [perms, setPerms] = useState<GroupPermState>(defaultPerms);

  const createGroup = async (e: FormEvent) => {
    e.preventDefault();
    setError(null);
    try {
      await apiPostJSON('/groups', { name, ldapGroupDn, oidcGroupClaim, ...perms });
      setName('');
      setLdapGroupDn('');
      setOidcGroupClaim('');
      setPerms(defaultPerms);
      await reloadGroups();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  const cb = (key: keyof GroupPermState, label: string): ReactNode => (
    <FormControlLabel
      key={key}
      control={
        <Checkbox
          checked={perms[key]}
          onChange={(e) => setPerms((p) => ({ ...p, [key]: e.target.checked }))}
        />
      }
      label={label}
    />
  );

  return (
    <Box>
      <Paper sx={{ p: 3, mb: 3 }}>
        <Typography variant="h6" component="h2" gutterBottom>
          Create group
        </Typography>
        <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
          Membership is never assigned here — it's fully derived from each member's LDAP{' '}
          <code>memberOf</code> or OIDC <code>groups</code> claim on every login. Set "LDAP group
          DN" and/or "OIDC groups claim value" below to link this group to a
          directory/identity-provider group; leave either blank if this group isn't sourced from
          that provider.
        </Typography>
        <Stack
          component="form"
          direction="row"
          spacing={2}
          useFlexGap
          sx={{ flexWrap: 'wrap', alignItems: 'center' }}
          onSubmit={createGroup}
        >
          <TextField
            label="Name"
            size="small"
            required
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
          <TextField
            label="LDAP group DN (optional)"
            size="small"
            placeholder="cn=editors,ou=groups,dc=example,dc=com"
            sx={{ minWidth: 280 }}
            value={ldapGroupDn}
            onChange={(e) => setLdapGroupDn(e.target.value)}
          />
          <TextField
            label="OIDC groups claim value (optional)"
            size="small"
            placeholder="e.g. editors"
            value={oidcGroupClaim}
            onChange={(e) => setOidcGroupClaim(e.target.value)}
          />
          {cb('canCreate', 'Can create')}
          {cb('canEdit', 'Can edit')}
          {cb('canDelete', 'Can delete')}
          {cb('canEditGeoObjects', 'Can edit geo objects')}
          {cb('canDeleteGeoObjects', 'Can delete geo objects')}
          {cb('canViewAll', 'Can view all maps')}
          {cb('isAdmin', 'Admin')}
          <Button type="submit" variant="contained">
            Create
          </Button>
        </Stack>
      </Paper>

      <Paper sx={{ p: 3 }}>
        <ErrorBanner message={error} />
        <TableContainer>
          <Table size="small">
            <TableHead>
              <TableRow>
                <TableCell>Name</TableCell>
                <TableCell align="center">Create</TableCell>
                <TableCell align="center">Edit</TableCell>
                <TableCell align="center">Delete</TableCell>
                <TableCell align="center">Geo edit</TableCell>
                <TableCell align="center">Geo delete</TableCell>
                <TableCell align="center">View all</TableCell>
                <TableCell align="center">Admin</TableCell>
                <TableCell>LDAP group DN</TableCell>
                <TableCell>OIDC groups claim value</TableCell>
                <TableCell>Created</TableCell>
                <TableCell>Actions</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {groups.map((g) => (
                <GroupRow key={g.id} g={g} onReload={reloadGroups} />
              ))}
            </TableBody>
          </Table>
        </TableContainer>
        {groups.length === 0 && (
          <Typography variant="body2" color="text.secondary" sx={{ mt: 2 }}>
            No groups yet.
          </Typography>
        )}
        <Typography variant="body2" color="text.secondary" sx={{ mt: 2 }}>
          Every member of a group inherits its permission flags (OR'd with their own) and any
          per-map grants made to the group under a map's "Permissions" — see "Group permissions"
          there.
        </Typography>
      </Paper>
    </Box>
  );
}
