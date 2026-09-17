import { useEffect, useState, type FormEvent } from "react";
import { apiFetch, apiJson, apiPostJSON } from "../../api/client";
import type { SyncRemote } from "../../api/types";
import ErrorBanner from "../../components/ErrorBanner";
import { fmtDate } from "../../lib/format";
import SyncLogModal from "./SyncLogModal";
import SyncMapsPickerModal from "./SyncMapsPickerModal";

function ServerPublicKeyCard() {
  const [key, setKey] = useState("");
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    apiJson<{ publicKeyPem: string }>("/server/public-key")
      .then((data) => setKey(data.publicKeyPem))
      .catch((err) => setError(err instanceof Error ? err.message : String(err)));
  }, []);

  return (
    <div className="card">
      <h2>This server's public key</h2>
      <p className="muted" style={{ marginTop: 0, marginBottom: "0.9rem" }}>
        Generated once on first startup and persisted across restarts. Every sync remote below authenticates with this same key —
        register it as an API key for a user on each remote tileserve-go instance you want to pull from (via that instance's "API keys"
        button on its Users tab), then paste the key ID that call returns into "Remote API key ID" when registering the remote.
      </p>
      <textarea readOnly rows={6} style={{ width: "100%", fontFamily: "monospace", fontSize: "0.8em" }} value={key} />
      <ErrorBanner message={error} />
    </div>
  );
}

function fmtSyncStatus(r: SyncRemote) {
  if (!r.lastSyncAt) return <span className="muted">never synced</span>;
  const status = r.lastSyncStatus === "error" ? <span style={{ color: "#dc2626" }}>error</span> : r.lastSyncStatus || "-";
  return (
    <>
      {status}
      <br />
      <span className="muted">{fmtDate(r.lastSyncAt)}</span>
      {r.lastSyncError && (
        <>
          <br />
          <span className="muted" title={r.lastSyncError}>
            {r.lastSyncError.length > 60 ? r.lastSyncError.slice(0, 60) + "…" : r.lastSyncError}
          </span>
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
        method: "PUT",
        headers: { "Content-Type": "application/json" },
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
    const name = prompt("Name", r.name);
    if (name === null) return;
    const baseUrl = prompt("Base URL", r.baseUrl);
    if (baseUrl === null) return;
    const remoteApiKeyId = prompt("Remote API key ID", r.remoteApiKeyId);
    if (remoteApiKeyId === null) return;
    const pollIntervalSecStr = prompt("Poll interval (seconds)", String(r.pollIntervalSec));
    if (pollIntervalSecStr === null) return;
    setError(null);
    try {
      await apiFetch(`/sync/remotes/${r.id}`, {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
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
      await apiFetch(`/sync/remotes/${r.id}/trigger`, { method: "POST" });
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  const remove = async () => {
    if (!confirm("Remove this sync remote? Already-mirrored maps keep their local data.")) return;
    setError(null);
    try {
      await apiFetch(`/sync/remotes/${r.id}`, { method: "DELETE" });
      onReload();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  return (
    <tr>
      <td>
        {r.name}
        <ErrorBanner message={error} />
      </td>
      <td>{r.baseUrl}</td>
      <td>{r.pollIntervalSec}s</td>
      <td>{r.syncAllMaps ? "All maps" : "Selected maps"}</td>
      <td className="checkbox-cell">
        <input type="checkbox" checked={r.enabled} onChange={(e) => updatePatch({ enabled: e.target.checked })} />
      </td>
      <td className="checkbox-cell">
        <input type="checkbox" checked={r.syncGeoObjects} onChange={(e) => updatePatch({ syncGeoObjects: e.target.checked })} />
      </td>
      <td>{fmtSyncStatus(r)}</td>
      <td>
        <div className="actions">
          <button type="button" className="secondary" onClick={edit}>
            Edit
          </button>
          <button type="button" className="secondary" onClick={onOpenMapsPicker}>
            Select maps
          </button>
          <button type="button" className="secondary" onClick={trigger}>
            Sync now
          </button>
          <button type="button" className="secondary" onClick={onOpenLog}>
            Logs
          </button>
          <button type="button" className="danger" onClick={remove}>
            Delete
          </button>
        </div>
      </td>
    </tr>
  );
}

export default function SyncTab() {
  const [remotes, setRemotes] = useState<SyncRemote[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [logRemote, setLogRemote] = useState<SyncRemote | null>(null);
  const [mapsPickerRemote, setMapsPickerRemote] = useState<SyncRemote | null>(null);

  const [name, setName] = useState("");
  const [baseUrl, setBaseUrl] = useState("");
  const [remoteApiKeyId, setRemoteApiKeyId] = useState("");
  const [pollIntervalSec, setPollIntervalSec] = useState("300");
  const [enabled, setEnabled] = useState(true);

  const reload = async () => {
    setError(null);
    try {
      setRemotes(await apiJson<SyncRemote[]>("/sync/remotes"));
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
      await apiPostJSON("/sync/remotes", {
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
      setName("");
      setBaseUrl("");
      setRemoteApiKeyId("");
      setPollIntervalSec("300");
      setEnabled(true);
      await reload();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  return (
    <div>
      <ServerPublicKeyCard />

      <div className="card">
        <h2>Register sync remote</h2>
        <p className="muted" style={{ marginTop: 0, marginBottom: "0.9rem" }}>
          Pulls a full mirror of every map visible to a registered API key on another tileserve-go instance — versions and aliases
          included, under the same map UUIDs. This server signs its own short-lived JWTs using its persistent key pair shown above;
          register that public key as an API key for a user on the remote instance, then paste the key ID that call returns into
          "Remote API key ID" below.
        </p>
        <form className="row" onSubmit={createRemote}>
          <div className="field">
            <label htmlFor="sync-name">Name</label>
            <input id="sync-name" required value={name} onChange={(e) => setName(e.target.value)} />
          </div>
          <div className="field">
            <label htmlFor="sync-base-url">Base URL</label>
            <input
              id="sync-base-url"
              placeholder="https://source.example.com"
              required
              value={baseUrl}
              onChange={(e) => setBaseUrl(e.target.value)}
            />
          </div>
          <div className="field">
            <label htmlFor="sync-remote-key-id">Remote API key ID</label>
            <input
              id="sync-remote-key-id"
              className="inline"
              placeholder="uuid returned by the remote"
              required
              value={remoteApiKeyId}
              onChange={(e) => setRemoteApiKeyId(e.target.value)}
            />
          </div>
          <div className="field">
            <label htmlFor="sync-interval">Poll interval (s)</label>
            <input
              id="sync-interval"
              type="number"
              min={1}
              required
              value={pollIntervalSec}
              onChange={(e) => setPollIntervalSec(e.target.value)}
            />
          </div>
          <div className="field">
            <label>
              <input type="checkbox" checked={enabled} onChange={(e) => setEnabled(e.target.checked)} /> Enabled
            </label>
          </div>
          <button type="submit">Register</button>
        </form>
      </div>

      <div className="card">
        <ErrorBanner message={error} />
        <table>
          <thead>
            <tr>
              <th>Name</th>
              <th>Base URL</th>
              <th>Interval</th>
              <th>Maps synced</th>
              <th className="checkbox-cell">Enabled</th>
              <th className="checkbox-cell">Geo objects</th>
              <th>Last sync</th>
              <th>Actions</th>
            </tr>
          </thead>
          <tbody>
            {remotes.map((r) => (
              <SyncRemoteRow
                key={r.id}
                r={r}
                onReload={reload}
                onOpenLog={() => setLogRemote(r)}
                onOpenMapsPicker={() => setMapsPickerRemote(r)}
              />
            ))}
          </tbody>
        </table>
        {remotes.length === 0 && <p className="muted">No sync remotes configured yet.</p>}
        <p className="muted">
          Removing a remote stops future syncing; maps already mirrored from it keep their local data. Deletions on the remote are never
          propagated here.
        </p>
      </div>

      <SyncLogModal remote={logRemote} onClose={() => setLogRemote(null)} />
      <SyncMapsPickerModal remote={mapsPickerRemote} onClose={() => setMapsPickerRemote(null)} onSaved={reload} />
    </div>
  );
}
