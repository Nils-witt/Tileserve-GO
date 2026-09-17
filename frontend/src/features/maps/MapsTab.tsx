import { useEffect, useState, type FormEvent } from 'react';
import { apiFetch, apiJson } from '../../api/client';
import { formatBytes, uploadMapVersion } from '../../api/upload';
import type { MapSummary, MapVersion } from '../../api/types';
import { useAdminData } from '../AdminDataContext';
import ErrorBanner from '../../components/ErrorBanner';
import { fmtDate } from '../../lib/format';
import PreviewModal from './PreviewModal';
import PermissionsModal from './PermissionsModal';
import AliasesModal from './AliasesModal';
import GeoObjectsModal from './GeoObjectsModal';

function VersionsPanel({ mapId, isAdmin }: { mapId: string; isAdmin: boolean }) {
  const [versions, setVersions] = useState<MapVersion[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  const download = async (version: string) => {
    try {
      const res = await apiFetch(`/maps/${mapId}/version/${encodeURIComponent(version)}/download`);
      const blob = await res.blob();
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `${mapId}-v${version}.zip`;
      document.body.appendChild(a);
      a.click();
      a.remove();
      URL.revokeObjectURL(url);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  useEffect(() => {
    apiJson<MapVersion[]>(`/maps/${mapId}/versions`)
      .then(setVersions)
      .catch((err) => {
        setError(err instanceof Error ? err.message : String(err));
        setVersions([]);
      });
  }, [mapId]);

  if (versions === null) return null;

  return (
    <div className="versions">
      <ErrorBanner message={error} />
      {versions.length === 0 ? (
        <span className="muted">No versions uploaded yet.</span>
      ) : (
        <ul>
          {versions.map((v) => (
            <li key={v.version}>
              v{v.version} — {fmtDate(v.createdAt)} by {v.createdBy}{' '}
              {isAdmin && (
                <button type="button" className="secondary" onClick={() => download(v.version)}>
                  Download
                </button>
              )}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

function MapRow({
  m,
  isAdmin,
  onReload,
}: {
  m: MapSummary;
  isAdmin: boolean;
  onReload: () => void;
}) {
  const [error, setError] = useState<string | null>(null);
  const [showVersions, setShowVersions] = useState(false);
  const [uploading, setUploading] = useState(false);
  const [progress, setProgress] = useState<{ loaded: number; total: number } | null>(null);
  const [previewOpen, setPreviewOpen] = useState(false);
  const [permOpen, setPermOpen] = useState(false);
  const [aliasOpen, setAliasOpen] = useState(false);
  const [geoOpen, setGeoOpen] = useState(false);

  const updateFlag = async (
    patch: Partial<Pick<MapSummary, 'visibleToAll' | 'anonymousAllowed'>>,
  ) => {
    setError(null);
    try {
      await apiFetch(`/maps/${m.uuid}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          name: m.name,
          currentVersion: m.currentVersion,
          visibleToAll: m.visibleToAll,
          anonymousAllowed: m.anonymousAllowed,
          ...patch,
        }),
      });
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
    onReload();
  };

  const editMap = async () => {
    const name = prompt('Name', m.name);
    if (name === null) return;
    const currentVersion = prompt('Current version', m.currentVersion || '');
    if (currentVersion === null) return;
    setError(null);
    try {
      await apiFetch(`/maps/${m.uuid}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          name,
          currentVersion,
          visibleToAll: m.visibleToAll,
          anonymousAllowed: m.anonymousAllowed,
        }),
      });
      onReload();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  const deleteMap = async () => {
    if (!confirm('Delete this map and all its versions?')) return;
    setError(null);
    try {
      await apiFetch(`/maps/${m.uuid}`, { method: 'DELETE' });
      onReload();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  const transferOwner = async () => {
    const newOwner = prompt(`Transfer ownership of "${m.name}" to username:`, m.owner);
    if (!newOwner || newOwner === m.owner) return;
    setError(null);
    try {
      await apiFetch(`/maps/${m.uuid}/owner`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ owner: newOwner }),
      });
      onReload();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  const handleUpload = async (file: File | undefined) => {
    if (!file) return;
    setError(null);
    setUploading(true);
    setProgress({ loaded: 0, total: 0 });
    const result = await uploadMapVersion(m.uuid, file, setProgress);
    setUploading(false);
    setProgress(null);
    if (!result.ok) {
      if (!result.unauthorized)
        setError(result.errorText || `request failed with status ${result.status}`);
    } else {
      onReload();
    }
  };

  return (
    <>
      <tr>
        <td>
          {m.name}
          <br />
          <span className="muted">{m.uuid}</span>
        </td>
        <td>{m.currentVersion || '-'}</td>
        <td className="checkbox-cell">
          <input
            type="checkbox"
            checked={m.visibleToAll}
            title="Visible to all users"
            onChange={(e) => updateFlag({ visibleToAll: e.target.checked })}
          />
        </td>
        <td className="checkbox-cell">
          <input
            type="checkbox"
            checked={m.anonymousAllowed}
            title="Allow fetching tile files without signing in"
            onChange={(e) => updateFlag({ anonymousAllowed: e.target.checked })}
          />
        </td>
        <td>{m.owner}</td>
        <td>
          {fmtDate(m.createdAt)}
          <br />
          <span className="muted">by {m.createdBy}</span>
        </td>
        <td>
          {fmtDate(m.updatedAt)}
          <br />
          <span className="muted">by {m.updatedBy}</span>
        </td>
        <td>
          <div className="actions">
            <button type="button" className="secondary" onClick={editMap}>
              Edit
            </button>
            <button
              type="button"
              className="secondary"
              disabled={uploading}
              onClick={() => document.getElementById(`upload-${m.uuid}`)?.click()}
            >
              Upload version
            </button>
            <input
              id={`upload-${m.uuid}`}
              type="file"
              className="hidden"
              style={{ display: 'none' }}
              accept=".zip,.tar,.tar.gz,.tgz"
              onChange={(e) => {
                handleUpload(e.target.files?.[0]);
                e.target.value = '';
              }}
            />
            <button type="button" className="secondary" onClick={() => setShowVersions((v) => !v)}>
              Versions
            </button>
            <button
              type="button"
              className="secondary"
              disabled={!m.currentVersion}
              title={!m.currentVersion ? 'No uploaded version yet' : undefined}
              onClick={() => setPreviewOpen(true)}
            >
              Preview
            </button>
            {progress && (
              <div className="upload-progress">
                <progress
                  value={progress.total ? progress.loaded : undefined}
                  max={progress.total || 100}
                />
                <span className="upload-progress-label">
                  {progress.total
                    ? `${formatBytes(progress.loaded)} / ${formatBytes(progress.total)}`
                    : 'Uploading…'}
                </span>
              </div>
            )}
            <button type="button" className="secondary" onClick={() => setPermOpen(true)}>
              Permissions
            </button>
            <button type="button" className="secondary" onClick={transferOwner}>
              Transfer owner
            </button>
            <button type="button" className="secondary" onClick={() => setAliasOpen(true)}>
              Aliases
            </button>
            <button
              type="button"
              className="secondary"
              disabled={!m.currentVersion}
              title={!m.currentVersion ? 'No uploaded version yet' : undefined}
              onClick={() => setGeoOpen(true)}
            >
              Geo objects
            </button>
            <button type="button" className="danger" onClick={deleteMap}>
              Delete
            </button>
          </div>
          <ErrorBanner message={error} />
          {showVersions && <VersionsPanel mapId={m.uuid} isAdmin={isAdmin} />}
        </td>
      </tr>
      <PreviewModal map={previewOpen ? m : null} onClose={() => setPreviewOpen(false)} />
      <PermissionsModal map={permOpen ? m : null} onClose={() => setPermOpen(false)} />
      <AliasesModal map={aliasOpen ? m : null} onClose={() => setAliasOpen(false)} />
      <GeoObjectsModal map={geoOpen ? m : null} onClose={() => setGeoOpen(false)} />
    </>
  );
}

export default function MapsTab() {
  const { maps, isAdmin, reloadMaps } = useAdminData();
  const [error, setError] = useState<string | null>(null);
  const [name, setName] = useState('');
  const [currentVersion, setCurrentVersion] = useState('');
  const [visibleToAll, setVisibleToAll] = useState(false);
  const [anonymousAllowed, setAnonymousAllowed] = useState(false);

  const createMap = async (e: FormEvent) => {
    e.preventDefault();
    setError(null);
    try {
      await apiFetch('/maps', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name, currentVersion, visibleToAll, anonymousAllowed }),
      });
      setName('');
      setCurrentVersion('');
      setVisibleToAll(false);
      setAnonymousAllowed(false);
      await reloadMaps();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  return (
    <div>
      <div className="card">
        <h2>Create map</h2>
        <form className="row" onSubmit={createMap}>
          <div className="field">
            <label htmlFor="create-name">Name</label>
            <input
              id="create-name"
              required
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
          </div>
          <div className="field">
            <label htmlFor="create-version">Current version (optional)</label>
            <input
              id="create-version"
              value={currentVersion}
              onChange={(e) => setCurrentVersion(e.target.value)}
            />
          </div>
          <div className="field">
            <label>
              <input
                type="checkbox"
                checked={visibleToAll}
                onChange={(e) => setVisibleToAll(e.target.checked)}
              />{' '}
              Visible to all
            </label>
          </div>
          <div className="field">
            <label>
              <input
                type="checkbox"
                checked={anonymousAllowed}
                onChange={(e) => setAnonymousAllowed(e.target.checked)}
              />{' '}
              Anonymous tile access
            </label>
          </div>
          <button type="submit">Create</button>
        </form>
      </div>

      <div className="card">
        <ErrorBanner message={error} />
        <table>
          <thead>
            <tr>
              <th>Name</th>
              <th>Version</th>
              <th className="checkbox-cell">Public</th>
              <th className="checkbox-cell">Anonymous</th>
              <th>Owner</th>
              <th>Created</th>
              <th>Updated</th>
              <th>Actions</th>
            </tr>
          </thead>
          <tbody>
            {maps.map((m) => (
              <MapRow key={m.uuid} m={m} isAdmin={isAdmin} onReload={reloadMaps} />
            ))}
          </tbody>
        </table>
        {maps.length === 0 && <p className="muted">No maps yet.</p>}
        <p className="muted">
          Maps are private by default — only admins, the owner, and users granted access under
          "Permissions" can see them, unless "Public" is checked. "Anonymous" separately controls
          whether tile files (<code>/maps/&lt;uuid&gt;/version/&lt;version&gt;/...</code>) can be
          fetched without signing in at all, regardless of "Public".
        </p>
      </div>
    </div>
  );
}
