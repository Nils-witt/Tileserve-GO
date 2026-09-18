import { useEffect, useState, type FormEvent } from 'react';
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
import { apiFetch, apiJson, apiPostJSON } from '../../api/client';
import type { SyncRemote } from '../../api/types';
import ErrorBanner from '../../components/ErrorBanner';
import { fmtDate } from '../../lib/format';
import SyncLogModal from './SyncLogModal';
import SyncMapsPickerModal from './SyncMapsPickerModal';

function ServerPublicKeyCard() {
  const [key, setKey] = useState('');
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    apiJson<{ publicKeyPem: string }>('/server/public-key')
      .then((data) => setKey(data.publicKeyPem))
      .catch((err) => setError(err instanceof Error ? err.message : String(err)));
  }, []);

  return (
    <Paper sx={{ p: 3, mb: 3 }}>
      <Typography variant="h6" component="h2" gutterBottom>
        This server's public key
      </Typography>
      <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
        Generated once on first startup and persisted across restarts. Every sync remote below
        authenticates with this same key — register it as an API key for a user on each remote
        tileserve-go instance you want to pull from (via that instance's "API keys" button on its
        Users tab), then paste the key ID that call returns into "Remote API key ID" when
        registering the remote.
      </Typography>
      <TextField
        fullWidth
        multiline
        minRows={6}
        value={key}
        slotProps={{
          input: { readOnly: true, style: { fontFamily: 'monospace', fontSize: '0.8em' } },
        }}
      />
      <ErrorBanner message={error} />
    </Paper>
  );
}

function fmtSyncStatus(r: SyncRemote) {
  if (!r.lastSyncAt)
    return (
      <Typography variant="body2" color="text.secondary">
        never synced
      </Typography>
    );
  const status =
    r.lastSyncStatus === 'error' ? (
      <Typography component="span" color="error.main">
        error
      </Typography>
    ) : (
      r.lastSyncStatus || '-'
    );
  return (
    <>
      {status}
      <br />
      <Typography variant="caption" color="text.secondary">
        {fmtDate(r.lastSyncAt)}
      </Typography>
      {r.lastSyncError && (
        <>
          <br />
          <Typography variant="caption" color="text.secondary" title={r.lastSyncError}>
            {r.lastSyncError.length > 60 ? r.lastSyncError.slice(0, 60) + '…' : r.lastSyncError}
          </Typography>
        </>
      )}
    </>
  );
}

function SyncRemoteRow({
  r,
  onReload,
  onOpenLog,
  onOpenMapsPicker,
}: {
  r: SyncRemote;
  onReload: () => void;
  onOpenLog: () => void;
  onOpenMapsPicker: () => void;
}) {
  const [error, setError] = useState<string | null>(null);

  const updatePatch = async (patch: Partial<SyncRemote>) => {
    setError(null);
    try {
      await apiFetch(`/sync/remotes/${r.id}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          name: r.name,
          baseUrl: r.baseUrl,
          remoteApiKeyId: r.remoteApiKeyId,
          pollIntervalSec: r.pollIntervalSec,
          enabled: r.enabled,
          syncAllMaps: r.syncAllMaps,
          syncNewMaps: r.syncNewMaps,
          syncGeoObjects: r.syncGeoObjects,
          ...patch,
        }),
      });
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
    onReload();
  };

  const edit = async () => {
    const name = prompt('Name', r.name);
    if (name === null) return;
    const baseUrl = prompt('Base URL', r.baseUrl);
    if (baseUrl === null) return;
    const remoteApiKeyId = prompt('Remote API key ID', r.remoteApiKeyId);
    if (remoteApiKeyId === null) return;
    const pollIntervalSecStr = prompt('Poll interval (seconds)', String(r.pollIntervalSec));
    if (pollIntervalSecStr === null) return;
    setError(null);
    try {
      await apiFetch(`/sync/remotes/${r.id}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          name,
          baseUrl,
          remoteApiKeyId,
          pollIntervalSec: parseInt(pollIntervalSecStr, 10),
          enabled: r.enabled,
          syncAllMaps: r.syncAllMaps,
          syncNewMaps: r.syncNewMaps,
          syncGeoObjects: r.syncGeoObjects,
        }),
      });
      onReload();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  const trigger = async () => {
    setError(null);
    try {
      await apiFetch(`/sync/remotes/${r.id}/trigger`, { method: 'POST' });
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  const remove = async () => {
    if (!confirm('Remove this sync remote? Already-mirrored maps keep their local data.')) return;
    setError(null);
    try {
      await apiFetch(`/sync/remotes/${r.id}`, { method: 'DELETE' });
      onReload();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  return (
    <TableRow>
      <TableCell>
        {r.name}
        <ErrorBanner message={error} />
      </TableCell>
      <TableCell>{r.baseUrl}</TableCell>
      <TableCell>{r.pollIntervalSec}s</TableCell>
      <TableCell>{r.syncAllMaps ? 'All maps' : 'Selected maps'}</TableCell>
      <TableCell align="center" padding="checkbox">
        <Checkbox
          checked={r.enabled}
          onChange={(e) => updatePatch({ enabled: e.target.checked })}
        />
      </TableCell>
      <TableCell align="center" padding="checkbox">
        <Checkbox
          checked={r.syncGeoObjects}
          onChange={(e) => updatePatch({ syncGeoObjects: e.target.checked })}
        />
      </TableCell>
      <TableCell>{fmtSyncStatus(r)}</TableCell>
      <TableCell sx={{ minWidth: 260 }}>
        <Stack direction="row" spacing={1} useFlexGap sx={{ flexWrap: 'wrap' }}>
          <Button size="small" onClick={edit}>
            Edit
          </Button>
          <Button size="small" onClick={onOpenMapsPicker}>
            Select maps
          </Button>
          <Button size="small" onClick={trigger}>
            Sync now
          </Button>
          <Button size="small" onClick={onOpenLog}>
            Logs
          </Button>
          <Button size="small" color="error" onClick={remove}>
            Delete
          </Button>
        </Stack>
      </TableCell>
    </TableRow>
  );
}

export default function SyncTab() {
  const [remotes, setRemotes] = useState<SyncRemote[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [logRemote, setLogRemote] = useState<SyncRemote | null>(null);
  const [mapsPickerRemote, setMapsPickerRemote] = useState<SyncRemote | null>(null);

  const [name, setName] = useState('');
  const [baseUrl, setBaseUrl] = useState('');
  const [remoteApiKeyId, setRemoteApiKeyId] = useState('');
  const [pollIntervalSec, setPollIntervalSec] = useState('300');
  const [enabled, setEnabled] = useState(true);

  const reload = async () => {
    setError(null);
    try {
      setRemotes(await apiJson<SyncRemote[]>('/sync/remotes'));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  useEffect(() => {
    reload();
  }, []);

  const createRemote = async (e: FormEvent) => {
    e.preventDefault();
    setError(null);
    try {
      await apiPostJSON('/sync/remotes', {
        name,
        baseUrl,
        remoteApiKeyId,
        pollIntervalSec: parseInt(pollIntervalSec, 10),
        enabled,
        // A freshly registered remote starts in "sync everything" mode;
        // use "Select maps" afterwards to narrow it down.
        syncAllMaps: true,
        syncNewMaps: false,
        // Geo-object syncing starts opted out too.
        syncGeoObjects: false,
      });
      setName('');
      setBaseUrl('');
      setRemoteApiKeyId('');
      setPollIntervalSec('300');
      setEnabled(true);
      await reload();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  return (
    <Box>
      <ServerPublicKeyCard />

      <Paper sx={{ p: 3, mb: 3 }}>
        <Typography variant="h6" component="h2" gutterBottom>
          Register sync remote
        </Typography>
        <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
          Pulls a full mirror of every map visible to a registered API key on another tileserve-go
          instance — versions and aliases included, under the same map UUIDs. This server signs its
          own short-lived JWTs using its persistent key pair shown above; register that public key
          as an API key for a user on the remote instance, then paste the key ID that call returns
          into "Remote API key ID" below.
        </Typography>
        <Stack
          component="form"
          direction="row"
          spacing={2}
          useFlexGap
          sx={{ flexWrap: 'wrap', alignItems: 'center' }}
          onSubmit={createRemote}
        >
          <TextField
            label="Name"
            size="small"
            required
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
          <TextField
            label="Base URL"
            size="small"
            placeholder="https://source.example.com"
            required
            sx={{ minWidth: 240 }}
            value={baseUrl}
            onChange={(e) => setBaseUrl(e.target.value)}
          />
          <TextField
            label="Remote API key ID"
            size="small"
            placeholder="uuid returned by the remote"
            required
            sx={{ minWidth: 220 }}
            value={remoteApiKeyId}
            onChange={(e) => setRemoteApiKeyId(e.target.value)}
          />
          <TextField
            label="Poll interval (s)"
            type="number"
            size="small"
            required
            slotProps={{ htmlInput: { min: 1 } }}
            value={pollIntervalSec}
            onChange={(e) => setPollIntervalSec(e.target.value)}
          />
          <FormControlLabel
            control={<Checkbox checked={enabled} onChange={(e) => setEnabled(e.target.checked)} />}
            label="Enabled"
          />
          <Button type="submit" variant="contained">
            Register
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
                <TableCell>Base URL</TableCell>
                <TableCell>Interval</TableCell>
                <TableCell>Maps synced</TableCell>
                <TableCell align="center">Enabled</TableCell>
                <TableCell align="center">Geo objects</TableCell>
                <TableCell>Last sync</TableCell>
                <TableCell>Actions</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {remotes.map((r) => (
                <SyncRemoteRow
                  key={r.id}
                  r={r}
                  onReload={reload}
                  onOpenLog={() => setLogRemote(r)}
                  onOpenMapsPicker={() => setMapsPickerRemote(r)}
                />
              ))}
            </TableBody>
          </Table>
        </TableContainer>
        {remotes.length === 0 && (
          <Typography variant="body2" color="text.secondary" sx={{ mt: 2 }}>
            No sync remotes configured yet.
          </Typography>
        )}
        <Typography variant="body2" color="text.secondary" sx={{ mt: 2 }}>
          Removing a remote stops future syncing; maps already mirrored from it keep their local
          data. Deletions on the remote are never propagated here.
        </Typography>
      </Paper>

      <SyncLogModal remote={logRemote} onClose={() => setLogRemote(null)} />
      <SyncMapsPickerModal
        remote={mapsPickerRemote}
        onClose={() => setMapsPickerRemote(null)}
        onSaved={reload}
      />
    </Box>
  );
}
