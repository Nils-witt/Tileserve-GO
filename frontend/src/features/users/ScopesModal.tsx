import { useEffect, useState } from 'react';
import {
  Button,
  FormControl,
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
  TextField,
  Typography,
} from '@mui/material';
import { apiFetch } from '../../api/client';
import type { ApiKey } from '../../api/types';
import { useMaps } from '../maps/MapsContext';
import Modal from '../../components/Modal';
import ErrorBanner from '../../components/ErrorBanner';
import { ApiKeyScopesProvider, useApiKeyScopes } from './ApiKeyScopesContext';

function ScopesModalContent({
  username,
  apiKey,
  onClose,
  onScopesChanged,
}: {
  username: string | null;
  apiKey: ApiKey | null;
  onClose: () => void;
  onScopesChanged: () => void;
}) {
  const { maps, mapName } = useMaps();
  const { scopes, error: loadError, reloadScopes } = useApiKeyScopes();
  const [error, setError] = useState<string | null>(null);
  const [addMap, setAddMap] = useState('');
  const [addVersions, setAddVersions] = useState('');

  useEffect(() => {
    if (!apiKey) return;
    setAddMap('');
    setAddVersions('');
  }, [apiKey]);

  const addScope = async () => {
    if (!username || !apiKey || !addMap) return;
    setError(null);
    try {
      const versions = addVersions.trim()
        ? addVersions
            .split(',')
            .map((v) => v.trim())
            .filter(Boolean)
        : null;
      await apiFetch(
        `/users/${encodeURIComponent(username)}/api-keys/${apiKey.id}/scopes/${addMap}`,
        {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ versions }),
        },
      );
      setAddVersions('');
      await reloadScopes();
      onScopesChanged();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  const removeScope = async (mapId: string) => {
    if (
      !confirm(
        "Remove this map from the key's scope? If it's the last entry, the key will be locked out of every map until you add another entry or clear its scope entirely.",
      )
    )
      return;
    if (!username || !apiKey) return;
    setError(null);
    try {
      await apiFetch(
        `/users/${encodeURIComponent(username)}/api-keys/${apiKey.id}/scopes/${mapId}`,
        { method: 'DELETE' },
      );
      await reloadScopes();
      onScopesChanged();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  const clearScope = async () => {
    if (
      !confirm(
        'Clear all scope restrictions? This key will regain unrestricted access (same as its user).',
      )
    )
      return;
    if (!username || !apiKey) return;
    setError(null);
    try {
      await apiFetch(`/users/${encodeURIComponent(username)}/api-keys/${apiKey.id}/scopes`, {
        method: 'DELETE',
      });
      await reloadScopes();
      onScopesChanged();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  return (
    <Modal
      open={!!apiKey}
      title={apiKey ? `Scopes — ${apiKey.name || apiKey.id}` : 'Scopes'}
      onClose={onClose}
    >
      <ErrorBanner message={error ?? loadError} />
      <TableContainer>
        <Table size="small">
          <TableHead>
            <TableRow>
              <TableCell>Map</TableCell>
              <TableCell>Versions</TableCell>
              <TableCell>Actions</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {scopes.map((s) => (
              <TableRow key={s.mapUuid}>
                <TableCell>{mapName(s.mapUuid)}</TableCell>
                <TableCell>
                  {s.versions && s.versions.length ? s.versions.join(', ') : 'all'}
                </TableCell>
                <TableCell>
                  <Button size="small" color="error" onClick={() => removeScope(s.mapUuid)}>
                    Remove
                  </Button>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </TableContainer>
      {scopes.length === 0 && (
        <Typography variant="body2" color="text.secondary" sx={{ mt: 1 }}>
          No scope restrictions — this key can access every map its user can.
        </Typography>
      )}
      <Stack
        direction="row"
        spacing={2}
        useFlexGap
        sx={{ flexWrap: 'wrap', alignItems: 'flex-end', mt: 2 }}
      >
        <FormControl size="small" sx={{ minWidth: 180 }}>
          <InputLabel id="scope-add-map-label">Map</InputLabel>
          <Select
            labelId="scope-add-map-label"
            label="Map"
            value={addMap}
            onChange={(e) => setAddMap(e.target.value)}
          >
            <MenuItem value="">
              <em>None</em>
            </MenuItem>
            {maps.map((m) => (
              <MenuItem key={m.uuid} value={m.uuid}>
                {m.name}
              </MenuItem>
            ))}
          </Select>
        </FormControl>
        <TextField
          label="Versions (comma-separated, blank = all)"
          size="small"
          placeholder="e.g. 3,4"
          value={addVersions}
          onChange={(e) => setAddVersions(e.target.value)}
        />
        <Button variant="contained" disabled={!addMap} onClick={addScope}>
          Add
        </Button>
        <Button color="error" onClick={clearScope}>
          Clear all (unrestrict)
        </Button>
      </Stack>
      <Typography variant="body2" color="text.secondary" sx={{ mt: 2 }}>
        Once this key has any scope entry, it can only access the maps listed here — and, where a
        version list is given, only those versions of that map — regardless of what its user could
        otherwise do. Removing entries one at a time only ever narrows access further; a key with an
        entry removed down to zero remains locked out. Use "Clear all" to explicitly restore
        unrestricted access.
      </Typography>
    </Modal>
  );
}

export default function ScopesModal({
  username,
  apiKey,
  onClose,
  onScopesChanged,
}: {
  username: string | null;
  apiKey: ApiKey | null;
  onClose: () => void;
  onScopesChanged: () => void;
}) {
  return (
    <ApiKeyScopesProvider username={username} apiKey={apiKey}>
      <ScopesModalContent
        username={username}
        apiKey={apiKey}
        onClose={onClose}
        onScopesChanged={onScopesChanged}
      />
    </ApiKeyScopesProvider>
  );
}
