import { useCallback, useEffect, useState } from 'react';
import { apiFetch, apiJson } from '../../api/client';
import type { MapAlias, MapSummary } from '../../api/types';
import Modal from '../../components/Modal';
import ErrorBanner from '../../components/ErrorBanner';
import { fmtDate } from '../../lib/format';

export default function AliasesModal({
  map,
  onClose,
}: {
  map: MapSummary | null;
  onClose: () => void;
}) {
  const [aliases, setAliases] = useState<MapAlias[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [name, setName] = useState('');
  const [version, setVersion] = useState('');

  const load = useCallback(async () => {
    if (!map) return;
    setError(null);
    try {
      setAliases(await apiJson<MapAlias[]>(`/maps/${map.uuid}/aliases`));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }, [map]);

  useEffect(() => {
    if (!map) return;
    setName('');
    setVersion('');
    load();
  }, [map, load]);

  const save = async () => {
    if (!map || !name.trim() || !version.trim()) return;
    setError(null);
    try {
      await apiFetch(`/maps/${map.uuid}/aliases/${encodeURIComponent(name.trim())}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ version: version.trim() }),
      });
      setName('');
      setVersion('');
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  const remove = async (alias: string) => {
    if (!map) return;
    setError(null);
    try {
      await apiFetch(`/maps/${map.uuid}/aliases/${encodeURIComponent(alias)}`, {
        method: 'DELETE',
      });
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  return (
    <Modal open={!!map} title={map ? `Aliases — ${map.name}` : 'Aliases'} onClose={onClose}>
      <ErrorBanner message={error} />
      <table>
        <thead>
          <tr>
            <th>Alias</th>
            <th>Version</th>
            <th>Updated</th>
            <th>Actions</th>
          </tr>
        </thead>
        <tbody>
          {aliases.map((a) => (
            <tr key={a.alias}>
              <td>{a.alias}</td>
              <td>{a.version}</td>
              <td>
                {fmtDate(a.updatedAt)}
                <br />
                <span className="muted">by {a.updatedBy}</span>
              </td>
              <td>
                <div className="actions">
                  <button type="button" className="danger" onClick={() => remove(a.alias)}>
                    Delete
                  </button>
                </div>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      {aliases.length === 0 && <p className="muted">No aliases yet.</p>}
      <div className="row" style={{ marginTop: '1rem' }}>
        <div className="field">
          <label htmlFor="alias-add-name">Alias name</label>
          <input
            id="alias-add-name"
            placeholder="e.g. stable"
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
        </div>
        <div className="field">
          <label htmlFor="alias-add-version">Version</label>
          <input
            id="alias-add-version"
            placeholder="e.g. 7"
            value={version}
            onChange={(e) => setVersion(e.target.value)}
          />
        </div>
        <button type="button" onClick={save}>
          Save
        </button>
      </div>
    </Modal>
  );
}
