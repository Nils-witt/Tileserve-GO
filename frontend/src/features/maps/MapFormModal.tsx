import { useEffect, useState, type FormEvent } from 'react';
import { Button, Checkbox, FormControlLabel, Stack, Tab, Tabs, TextField } from '@mui/material';
import { apiFetch } from '../../api/client';
import type { MapSummary } from '../../api/types';
import Modal from '../../components/Modal';
import ErrorBanner from '../../components/ErrorBanner';
import PermissionsPanel from './PermissionsPanel';
import AliasesPanel from './AliasesPanel';
import TransferOwnerPanel from './TransferOwnerPanel';

type TabKey = 'details' | 'permissions' | 'aliases' | 'transfer';

export default function MapFormModal({
  open,
  map,
  onClose,
  onSaved,
}: {
  open: boolean;
  /** null creates a new map; a MapSummary edits that map. */
  map: MapSummary | null;
  onClose: () => void;
  onSaved: () => void | Promise<void>;
}) {
  const [tab, setTab] = useState<TabKey>('details');
  const [name, setName] = useState('');
  const [currentVersion, setCurrentVersion] = useState('');
  const [visibleToAll, setVisibleToAll] = useState(false);
  const [anonymousAllowed, setAnonymousAllowed] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    if (!open) return;
    setTab('details');
    setError(null);
    setName(map?.name ?? '');
    setCurrentVersion(map?.currentVersion ?? '');
    setVisibleToAll(map?.visibleToAll ?? false);
    setAnonymousAllowed(map?.anonymousAllowed ?? false);
  }, [open, map]);

  const save = async (e: FormEvent) => {
    e.preventDefault();
    setError(null);
    setSaving(true);
    try {
      await apiFetch(map ? `/maps/${map.uuid}` : '/maps', {
        method: map ? 'PUT' : 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name, currentVersion, visibleToAll, anonymousAllowed }),
      });
      await onSaved();
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  };

  return (
    <Modal open={open} title={map ? `Edit map — ${map.name}` : 'Create map'} onClose={onClose}>
      {map && (
        <Tabs
          value={tab}
          onChange={(_, v: TabKey) => setTab(v)}
          sx={{ mb: 2, borderBottom: 1, borderColor: 'divider' }}
        >
          <Tab value="details" label="Details" />
          <Tab value="permissions" label="Permissions" />
          <Tab value="aliases" label="Aliases" />
          <Tab value="transfer" label="Transfer owner" />
        </Tabs>
      )}
      {tab === 'details' && (
        <Stack component="form" spacing={2} onSubmit={save} sx={{ minWidth: 320 }}>
          <ErrorBanner message={error} />
          <TextField
            label="Name"
            required
            autoFocus
            size="small"
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
          <TextField
            label="Current version (optional)"
            size="small"
            value={currentVersion}
            onChange={(e) => setCurrentVersion(e.target.value)}
          />
          <FormControlLabel
            control={
              <Checkbox
                checked={visibleToAll}
                onChange={(e) => setVisibleToAll(e.target.checked)}
              />
            }
            label="Visible to all"
          />
          <FormControlLabel
            control={
              <Checkbox
                checked={anonymousAllowed}
                onChange={(e) => setAnonymousAllowed(e.target.checked)}
              />
            }
            label="Anonymous tile access"
          />
          <Stack direction="row" spacing={1} sx={{ justifyContent: 'flex-end' }}>
            <Button onClick={onClose}>Cancel</Button>
            <Button type="submit" variant="contained" disabled={saving}>
              {map ? 'Save' : 'Create'}
            </Button>
          </Stack>
        </Stack>
      )}
      {map && tab === 'permissions' && <PermissionsPanel map={map} />}
      {map && tab === 'aliases' && <AliasesPanel map={map} />}
      {map && tab === 'transfer' && <TransferOwnerPanel map={map} onSaved={onSaved} />}
    </Modal>
  );
}
