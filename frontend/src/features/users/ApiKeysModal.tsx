import { useCallback, useEffect, useState } from "react";
import { apiFetch, apiJson, apiPostJSON } from "../../api/client";
import type { ApiKey, GeneratedKeyPair } from "../../api/types";
import Modal from "../../components/Modal";
import ErrorBanner from "../../components/ErrorBanner";
import { fmtDate } from "../../lib/format";
import ScopesModal from "./ScopesModal";

export default function ApiKeysModal({ username, onClose }: { username: string | null; onClose: () => void }) {
  const [keys, setKeys] = useState<ApiKey[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [name, setName] = useState("");
  const [pubkey, setPubkey] = useState("");
  const [scopesKey, setScopesKey] = useState<ApiKey | null>(null);

  const load = useCallback(async () => {
    if (!username) return;
    setError(null);
    try {
      setKeys(await apiJson<ApiKey[]>(`/users/${encodeURIComponent(username)}/api-keys`));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }, [username]);

  useEffect(() => {
    if (!username) return;
    setName("");
    setPubkey("");
    load();
  }, [username, load]);

  const generateKeyPair = async () => {
    setError(null);
    try {
      const kp = await apiPostJSON<GeneratedKeyPair>("/keys/generate", {});
      // The private key is only ever available here, once — a blocking
      // prompt (pre-filled, selectable) is the simplest way to give the
      // admin a chance to copy it before it's gone for good.
      prompt("Save this private key now — it will not be shown again:", kp.privateKeyPem);
      setPubkey(kp.publicKeyPem);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  const createKey = async () => {
    if (!username || !name.trim() || !pubkey.trim()) return;
    setError(null);
    try {
      const created = await apiPostJSON<ApiKey>(`/users/${encodeURIComponent(username)}/api-keys`, {
        name: name.trim(),
        publicKeyPem: pubkey.trim(),
      });
      setName("");
      setPubkey("");
      await load();
      // Nothing secret comes back here — the server never sees a private
      // key — but the caller still needs to know which id to use as the
      // JWT `kid` when signing tokens for this key.
      alert('API key registered. Key ID (use as the JWT "kid" when signing tokens): ' + created.id);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  const revokeKey = async (id: string) => {
    if (!username) return;
    if (!confirm("Revoke this API key? Anything still using it will lose access immediately.")) return;
    setError(null);
    try {
      await apiFetch(`/users/${encodeURIComponent(username)}/api-keys/${id}`, { method: "DELETE" });
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  return (
    <>
      <Modal open={!!username} title={username ? `API keys — ${username}` : "API keys"} onClose={onClose}>
        <ErrorBanner message={error} />
        <table>
          <thead>
            <tr>
              <th>Name</th>
              <th>Created</th>
              <th>Last used</th>
              <th>Scoped</th>
              <th>Actions</th>
            </tr>
          </thead>
          <tbody>
            {keys.map((k) => (
              <tr key={k.id}>
                <td>{k.name || "-"}</td>
                <td>
                  {fmtDate(k.createdAt)}
                  <br />
                  <span className="muted">by {k.createdBy}</span>
                </td>
                <td>{k.lastUsedAt ? fmtDate(k.lastUsedAt) : "never"}</td>
                <td className={k.scoped ? undefined : "muted"}>{k.scoped ? "Yes" : "No"}</td>
                <td>
                  <div className="actions">
                    <button type="button" className="secondary" onClick={() => setScopesKey(k)}>
                      Scopes
                    </button>
                    <button type="button" className="danger" onClick={() => revokeKey(k.id)}>
                      Revoke
                    </button>
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        {keys.length === 0 && <p className="muted">No API keys yet.</p>}
        <div className="row" style={{ marginTop: "1rem" }}>
          <div className="field">
            <label htmlFor="apikey-add-name">Name</label>
            <input id="apikey-add-name" placeholder="e.g. edge-server-01 sync" value={name} onChange={(e) => setName(e.target.value)} />
          </div>
          <div className="field" style={{ flexBasis: "100%" }}>
            <label htmlFor="apikey-add-pubkey">Public key (PEM)</label>
            <textarea
              id="apikey-add-pubkey"
              rows={3}
              placeholder="-----BEGIN PUBLIC KEY-----..."
              style={{ width: "100%", fontFamily: "monospace", fontSize: "0.8em" }}
              value={pubkey}
              onChange={(e) => setPubkey(e.target.value)}
            />
          </div>
          <button type="button" className="secondary" onClick={generateKeyPair}>
            Generate key pair
          </button>
          <button type="button" disabled={!name.trim() || !pubkey.trim()} onClick={createKey}>
            Create
          </button>
        </div>
        <p className="muted">
          The key authenticates as this user (their full permissions apply, narrowed by "Scopes" below if any are set). The caller
          generates its own RSA key pair and signs short-lived JWTs with the private half — this server only ever stores the public
          half. Click "Generate key pair" to have this server generate one for you (the private key is shown exactly once, copy it
          immediately), or paste a public key generated elsewhere. Use the resulting key's ID as the "Remote API key ID" when
          registering this server as a sync remote on another instance.
        </p>
      </Modal>
      <ScopesModal username={username} apiKey={scopesKey} onClose={() => setScopesKey(null)} onScopesChanged={load} />
    </>
  );
}
