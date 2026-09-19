import { useEffect, useState } from 'react';
import {
  Button,
  Checkbox,
  FormControlLabel,
  Stack,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  Typography,
} from '@mui/material';
import { useApi } from '../../api/ApiContext';
import type { SyncRemote } from '../../api/types';
import Modal from '../../components/Modal';
import ErrorBanner from '../../components/ErrorBanner';
import { SyncRemoteMapsProvider, useSyncRemoteMaps } from './SyncRemoteMapsContext';

function SyncMapsPickerModalContent({
  remote,
  onClose,
  onSaved,
}: {
  remote: SyncRemote | null;
  onClose: () => void;
  onSaved: () => void;
}) {
  const api = useApi();
  const { remoteMaps, selectedMapUuids, error: loadError } = useSyncRemoteMaps();
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [syncAll, setSyncAll] = useState(false);
  const [syncNew, setSyncNew] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!remote) return;
    setSyncAll(remote.syncAllMaps);
    setSyncNew(remote.syncNewMaps);
    setError(null);
  }, [remote]);

  useEffect(() => {
    setSelected(new Set(selectedMapUuids));
  }, [selectedMapUuids]);

  const toggle = (uuid: string, checked: boolean) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (checked) next.add(uuid);
      else next.delete(uuid);
      return next;
    });
  };

  const save = async () => {
    if (!remote) return;
    setError(null);
    try {
      await api.updateSyncRemote(remote.id, {
        name: remote.name,
        baseUrl: remote.baseUrl,
        remoteApiKeyId: remote.remoteApiKeyId,
        pollIntervalSec: remote.pollIntervalSec,
        enabled: remote.enabled,
        syncAllMaps: syncAll,
        syncNewMaps: syncNew,
        syncGeoObjects: remote.syncGeoObjects,
        selectedMapUuids: Array.from(selected),
      });
      onSaved();
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  return (
    <Modal
      open={!!remote}
      title={remote ? `Select maps — ${remote.name}` : 'Select maps'}
      onClose={onClose}
      headerExtra={
        <Button size="small" variant="contained" onClick={save}>
          Save
        </Button>
      }
    >
      <ErrorBanner message={error ?? loadError} />
      <Stack direction="row" spacing={2} sx={{ mb: 1 }}>
        <FormControlLabel
          control={<Checkbox checked={syncAll} onChange={(e) => setSyncAll(e.target.checked)} />}
          label="Sync all maps"
        />
        <FormControlLabel
          control={
            <Checkbox
              checked={syncNew}
              disabled={syncAll}
              onChange={(e) => setSyncNew(e.target.checked)}
            />
          }
          label="Automatically sync new maps"
        />
      </Stack>
      <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
        With "Sync all maps" off, only the maps checked below are mirrored. "Automatically sync new
        maps" additionally mirrors any map first noticed on the remote from then on, even if it
        isn't checked.
      </Typography>
      <TableContainer>
        <Table size="small">
          <TableHead>
            <TableRow>
              <TableCell align="center">Sync</TableCell>
              <TableCell>Name</TableCell>
              <TableCell>UUID</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {remoteMaps.map((m) => (
              <TableRow key={m.uuid}>
                <TableCell align="center" padding="checkbox">
                  <Checkbox
                    checked={selected.has(m.uuid)}
                    onChange={(e) => toggle(m.uuid, e.target.checked)}
                  />
                </TableCell>
                <TableCell>{m.name}</TableCell>
                <TableCell>
                  <Typography variant="caption" color="text.secondary">
                    {m.uuid}
                  </Typography>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </TableContainer>
      {remoteMaps.length === 0 && (
        <Typography variant="body2" color="text.secondary" sx={{ mt: 1 }}>
          No maps visible to this remote's API key.
        </Typography>
      )}
    </Modal>
  );
}

export default function SyncMapsPickerModal({
  remote,
  onClose,
  onSaved,
}: {
  remote: SyncRemote | null;
  onClose: () => void;
  onSaved: () => void;
}) {
  return (
    <SyncRemoteMapsProvider remote={remote}>
      <SyncMapsPickerModalContent remote={remote} onClose={onClose} onSaved={onSaved} />
    </SyncRemoteMapsProvider>
  );
}
