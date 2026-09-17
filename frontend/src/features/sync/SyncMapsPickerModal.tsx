import { useEffect, useState } from 'react';
import { apiFetch, apiJson } from '../../api/client';
import type { RemoteMap, SyncRemote } from '../../api/types';
import Modal from '../../components/Modal';
import ErrorBanner from '../../components/ErrorBanner';

export default function SyncMapsPickerModal({
  remote,
  onClose,
  onSaved,
}: {
  remote: SyncRemote | null;
  onClose: () => void;
  onSaved: () => void;
}) {
  const [remoteMaps, setRemoteMaps] = useState<RemoteMap[]>([]);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [syncAll, setSyncAll] = useState(false);
  const [syncNew, setSyncNew] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!remote) return;
    setSyncAll(remote.syncAllMaps);
    setSyncNew(remote.syncNewMaps);
    setError(null);
    (async () => {
      try {
        const [maps, selectedIds] = await Promise.all([
          apiJson<RemoteMap[]>(`/sync/remotes/${remote.id}/remote-maps`),
          apiJson<string[]>(`/sync/remotes/${remote.id}/selected-maps`),
        ]);
        setRemoteMaps(maps);
        setSelected(new Set(selectedIds));
      } catch (err) {
        setError(err instanceof Error ? err.message : String(err));
      }
    })();
  }, [remote]);

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
      await apiFetch(`/sync/remotes/${remote.id}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          name: remote.name,
          baseUrl: remote.baseUrl,
          remoteApiKeyId: remote.remoteApiKeyId,
          pollIntervalSec: remote.pollIntervalSec,
          enabled: remote.enabled,
          syncAllMaps: syncAll,
          syncNewMaps: syncNew,
          syncGeoObjects: remote.syncGeoObjects,
          selectedMapUuids: Array.from(selected),
        }),
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
        <button type="button" onClick={save}>
          Save
        </button>
      }
    >
      <ErrorBanner message={error} />
      <div className="row">
        <div className="field">
          <label>
            <input
              type="checkbox"
              checked={syncAll}
              onChange={(e) => setSyncAll(e.target.checked)}
            />{' '}
            Sync all maps
          </label>
        </div>
        <div className="field">
          <label>
            <input
              type="checkbox"
              checked={syncNew}
              disabled={syncAll}
              onChange={(e) => setSyncNew(e.target.checked)}
            />{' '}
            Automatically sync new maps
          </label>
        </div>
      </div>
      <p className="muted" style={{ marginTop: 0 }}>
        With "Sync all maps" off, only the maps checked below are mirrored. "Automatically sync new
        maps" additionally mirrors any map first noticed on the remote from then on, even if it
        isn't checked.
      </p>
      <table>
        <thead>
          <tr>
            <th className="checkbox-cell">Sync</th>
            <th>Name</th>
            <th>UUID</th>
          </tr>
        </thead>
        <tbody>
          {remoteMaps.map((m) => (
            <tr key={m.uuid}>
              <td className="checkbox-cell">
                <input
                  type="checkbox"
                  checked={selected.has(m.uuid)}
                  onChange={(e) => toggle(m.uuid, e.target.checked)}
                />
              </td>
              <td>{m.name}</td>
              <td className="muted">{m.uuid}</td>
            </tr>
          ))}
        </tbody>
      </table>
      {remoteMaps.length === 0 && (
        <p className="muted">No maps visible to this remote's API key.</p>
      )}
    </Modal>
  );
}
