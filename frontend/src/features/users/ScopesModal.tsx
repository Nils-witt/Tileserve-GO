import { useCallback, useEffect, useState } from 'react';
import { apiFetch, apiJson } from '../../api/client';
import type { ApiKey, ApiKeyScope } from '../../api/types';
import { useAdminData } from '../AdminDataContext';
import Modal from '../../components/Modal';
import ErrorBanner from '../../components/ErrorBanner';

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
  const { maps, mapName } = useAdminData();
  const [scopes, setScopes] = useState<ApiKeyScope[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [addMap, setAddMap] = useState('');
  const [addVersions, setAddVersions] = useState('');

  const load = useCallback(async () => {
    if (!username || !apiKey) return;
    setError(null);
    try {
      setScopes(
        await apiJson<ApiKeyScope[]>(
          `/users/${encodeURIComponent(username)}/api-keys/${apiKey.id}/scopes`,
        ),
      );
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }, [username, apiKey]);

  useEffect(() => {
    if (!apiKey) return;
    setAddMap('');
    setAddVersions('');
    load();
  }, [apiKey, load]);

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
      await load();
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
      await load();
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
      await load();
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
      <ErrorBanner message={error} />
      <table>
        <thead>
          <tr>
            <th>Map</th>
            <th>Versions</th>
            <th>Actions</th>
          </tr>
        </thead>
        <tbody>
          {scopes.map((s) => (
            <tr key={s.mapUuid}>
              <td>{mapName(s.mapUuid)}</td>
              <td>{s.versions && s.versions.length ? s.versions.join(', ') : 'all'}</td>
              <td>
                <div className="actions">
                  <button type="button" className="danger" onClick={() => removeScope(s.mapUuid)}>
                    Remove
                  </button>
                </div>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      {scopes.length === 0 && (
        <p className="muted">No scope restrictions — this key can access every map its user can.</p>
      )}
      <div className="row" style={{ marginTop: '1rem' }}>
        <div className="field">
          <label htmlFor="scope-add-map">Map</label>
          <select id="scope-add-map" value={addMap} onChange={(e) => setAddMap(e.target.value)}>
            <option value="" />
            {maps.map((m) => (
              <option key={m.uuid} value={m.uuid}>
                {m.name}
              </option>
            ))}
          </select>
        </div>
        <div className="field">
          <label htmlFor="scope-add-versions">Versions (comma-separated, blank = all)</label>
          <input
            id="scope-add-versions"
            placeholder="e.g. 3,4"
            value={addVersions}
            onChange={(e) => setAddVersions(e.target.value)}
          />
        </div>
        <button type="button" disabled={!addMap} onClick={addScope}>
          Add
        </button>
        <button type="button" className="danger" onClick={clearScope}>
          Clear all (unrestrict)
        </button>
      </div>
      <p className="muted">
        Once this key has any scope entry, it can only access the maps listed here — and, where a
        version list is given, only those versions of that map — regardless of what its user could
        otherwise do. Removing entries one at a time only ever narrows access further; a key with an
        entry removed down to zero remains locked out. Use "Clear all" to explicitly restore
        unrestricted access.
      </p>
    </Modal>
  );
}
