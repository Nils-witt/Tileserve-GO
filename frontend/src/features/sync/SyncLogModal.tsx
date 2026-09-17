import { useCallback, useEffect, useState } from "react";
import { apiJson } from "../../api/client";
import type { SyncLogEntry, SyncRemote } from "../../api/types";
import Modal from "../../components/Modal";
import ErrorBanner from "../../components/ErrorBanner";
import { fmtDate } from "../../lib/format";

export default function SyncLogModal({ remote, onClose }: { remote: SyncRemote | null; onClose: () => void }) {
  const [entries, setEntries] = useState<SyncLogEntry[]>([]);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    if (!remote) return;
    setError(null);
    try {
      setEntries(await apiJson<SyncLogEntry[]>(`/sync/remotes/${remote.id}/logs`));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }, [remote]);

  useEffect(() => {
    load();
  }, [load]);

  // Newest entry first, since that's the one an admin checking on a sync is
  // almost always looking for. Error-level entries are highlighted so a
  // failure stands out among routine lines.
  const lines = entries
    .slice()
    .reverse()
    .map((e, i) => {
      const line = `[${fmtDate(e.time)}] ${e.message}`;
      return e.level === "error" ? (
        <span key={i} style={{ color: "#dc2626" }}>
          {line}
          {"\n"}
        </span>
      ) : (
        <span key={i}>
          {line}
          {"\n"}
        </span>
      );
    });

  return (
    <Modal
      open={!!remote}
      title={remote ? `Sync log — ${remote.name}` : "Sync log"}
      onClose={onClose}
      headerExtra={
        <button type="button" className="secondary" onClick={load}>
          Refresh
        </button>
      }
    >
      <ErrorBanner message={error} />
      <pre style={{ whiteSpace: "pre-wrap", fontFamily: "monospace", fontSize: "0.8em", margin: 0 }}>{lines}</pre>
      {entries.length === 0 && <p className="muted">No log entries yet.</p>}
    </Modal>
  );
}
