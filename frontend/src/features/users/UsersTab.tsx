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
import { useApi } from '../../api/ApiContext';
import type { User } from '../../api/types';
import { useUsers } from './UsersContext';
import { useAuth } from '../../auth/AuthContext';
import ErrorBanner from '../../components/ErrorBanner';
import { fmtDate } from '../../lib/format';
import ApiKeysModal from './ApiKeysModal';

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

function UserRow({
  u,
  self,
  onReload,
  onOpenApiKeys,
}: {
  u: User;
  self: boolean;
  onReload: () => void;
  onOpenApiKeys: () => void;
}) {
  const api = useApi();
  const [perms, setPerms] = useState<UserPermState>(u);
  const [password, setPassword] = useState('');
  const [error, setError] = useState<string | null>(null);

  const save = async () => {
    setError(null);
    try {
      await api.updateUser(u.username, { ...perms, password });
      await onReload();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  const remove = async () => {
    if (!confirm(`Delete user "${u.username}"?`)) return;
    setError(null);
    try {
      await api.deleteUser(u.username);
      await onReload();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  const cb = (key: keyof UserPermState) => (
    <Checkbox
      checked={perms[key]}
      onChange={(e) => setPerms((p) => ({ ...p, [key]: e.target.checked }))}
    />
  );

  return (
    <TableRow>
      <TableCell>
        {u.username}
        {self ? ' (you)' : ''}
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
          type="password"
          size="small"
          sx={{ width: '10rem' }}
          placeholder="leave blank to keep"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
        />
      </TableCell>
      <TableCell>{fmtDate(u.createdAt)}</TableCell>
      <TableCell sx={{ minWidth: 220 }}>
        <Stack direction="row" spacing={1} useFlexGap sx={{ flexWrap: 'wrap' }}>
          <Button size="small" onClick={save}>
            Save
          </Button>
          <Button size="small" onClick={onOpenApiKeys}>
            API keys
          </Button>
          <Button
            size="small"
            color="error"
            disabled={self}
            title={self ? "You can't delete your own account" : undefined}
            onClick={remove}
          >
            Delete
          </Button>
        </Stack>
      </TableCell>
    </TableRow>
  );
}

export default function UsersTab() {
  const api = useApi();
  const { users, reloadUsers } = useUsers();
  const { username: currentUsername } = useAuth();
  const [error, setError] = useState<string | null>(null);
  const [apiKeysUser, setApiKeysUser] = useState<string | null>(null);
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [createPerms, setCreatePerms] = useState<UserPermState>(defaultCreatePerms);

  const createUser = async (e: FormEvent) => {
    e.preventDefault();
    setError(null);
    try {
      await api.createUser({ username, password, ...createPerms });
      setUsername('');
      setPassword('');
      setCreatePerms(defaultCreatePerms);
      await reloadUsers();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  const cb = (key: keyof UserPermState, label: string): ReactNode => (
    <FormControlLabel
      key={key}
      control={
        <Checkbox
          checked={createPerms[key]}
          onChange={(e) => setCreatePerms((p) => ({ ...p, [key]: e.target.checked }))}
        />
      }
      label={label}
    />
  );

  return (
    <Box>
      <Paper sx={{ p: 3, mb: 3 }}>
        <Typography variant="h6" component="h2" gutterBottom>
          Create user
        </Typography>
        <Stack
          component="form"
          direction="row"
          spacing={2}
          useFlexGap
          sx={{ flexWrap: 'wrap', alignItems: 'center' }}
          onSubmit={createUser}
        >
          <TextField
            label="Username"
            size="small"
            required
            value={username}
            onChange={(e) => setUsername(e.target.value)}
          />
          <TextField
            label="Password"
            type="password"
            size="small"
            required
            value={password}
            onChange={(e) => setPassword(e.target.value)}
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
                <TableCell>Username</TableCell>
                <TableCell align="center">Create</TableCell>
                <TableCell align="center">Edit</TableCell>
                <TableCell align="center">Delete</TableCell>
                <TableCell align="center">Geo edit</TableCell>
                <TableCell align="center">Geo delete</TableCell>
                <TableCell align="center">View all</TableCell>
                <TableCell align="center">Admin</TableCell>
                <TableCell>New password</TableCell>
                <TableCell>Created</TableCell>
                <TableCell>Actions</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {users.map((u) => (
                <UserRow
                  key={u.username}
                  u={u}
                  self={u.username === currentUsername}
                  onReload={reloadUsers}
                  onOpenApiKeys={() => setApiKeysUser(u.username)}
                />
              ))}
            </TableBody>
          </Table>
        </TableContainer>
      </Paper>

      <ApiKeysModal username={apiKeysUser} onClose={() => setApiKeysUser(null)} />
    </Box>
  );
}
