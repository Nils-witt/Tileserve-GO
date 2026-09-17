import { useEffect, useState, type FormEvent } from 'react';
import { apiJson } from '../../api/client';
import type { AuditLogEntry } from '../../api/types';
import ErrorBanner from '../../components/ErrorBanner';
import { fmtDate } from '../../lib/format';

const PAGE_SIZE = 100;

interface Filter {
  actor: string;
  action: string;
  entityType: string;
  entityId: string;
}

const emptyFilter: Filter = { actor: '', action: '', entityType: '', entityId: '' };

export default function AuditTab() {
  const [filter, setFilter] = useState<Filter>(emptyFilter);
  const [entries, setEntries] = useState<AuditLogEntry[]>([]);
  const [offset, setOffset] = useState(0);
  const [hasMore, setHasMore] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const buildParams = (f: Filter, nextOffset: number) => {
    const params = new URLSearchParams();
    if (f.actor.trim()) params.set('actor', f.actor.trim());
    if (f.action.trim()) params.set('action', f.action.trim());
    if (f.entityType.trim()) params.set('entityType', f.entityType.trim());
    if (f.entityId.trim()) params.set('entityId', f.entityId.trim());
    params.set('limit', String(PAGE_SIZE));
    params.set('offset', String(nextOffset));
    return params;
  };

  const load = async (f: Filter, reset: boolean) => {
    setError(null);
    const nextOffset = reset ? 0 : offset;
    try {
      const page = await apiJson<AuditLogEntry[]>(
        '/audit-logs?' + buildParams(f, nextOffset).toString(),
      );
      setEntries((prev) => (reset ? page : [...prev, ...page]));
      setOffset(nextOffset + page.length);
      setHasMore(page.length === PAGE_SIZE);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  useEffect(() => {
    load(emptyFilter, true);
    // Only load once, on mount — subsequent loads are user-triggered.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const handleSubmit = (e: FormEvent) => {
    e.preventDefault();
    load(filter, true);
  };

  const handleClear = () => {
    setFilter(emptyFilter);
    load(emptyFilter, true);
  };

  return (
    <div>
      <div className="card">
        <form className="row" onSubmit={handleSubmit}>
          <div className="field">
            <label htmlFor="audit-filter-actor">Actor</label>
            <input
              id="audit-filter-actor"
              placeholder="username"
              value={filter.actor}
              onChange={(e) => setFilter((f) => ({ ...f, actor: e.target.value }))}
            />
          </div>
          <div className="field">
            <label htmlFor="audit-filter-action">Action</label>
            <input
              id="audit-filter-action"
              placeholder="e.g. create"
              value={filter.action}
              onChange={(e) => setFilter((f) => ({ ...f, action: e.target.value }))}
            />
          </div>
          <div className="field">
            <label htmlFor="audit-filter-entity-type">Entity type</label>
            <input
              id="audit-filter-entity-type"
              placeholder="e.g. map"
              value={filter.entityType}
              onChange={(e) => setFilter((f) => ({ ...f, entityType: e.target.value }))}
            />
          </div>
          <div className="field">
            <label htmlFor="audit-filter-entity-id">Entity ID</label>
            <input
              id="audit-filter-entity-id"
              value={filter.entityId}
              onChange={(e) => setFilter((f) => ({ ...f, entityId: e.target.value }))}
            />
          </div>
          <button type="submit">Filter</button>
          <button type="button" className="secondary" onClick={handleClear}>
            Clear
          </button>
        </form>
      </div>

      <div className="card">
        <ErrorBanner message={error} />
        <table>
          <thead>
            <tr>
              <th>Time</th>
              <th>Actor</th>
              <th>Action</th>
              <th>Entity type</th>
              <th>Entity ID</th>
              <th>Detail</th>
            </tr>
          </thead>
          <tbody>
            {entries.map((e, i) => (
              <tr key={i}>
                <td>{fmtDate(e.occurredAt)}</td>
                <td>{e.actor}</td>
                <td>{e.action}</td>
                <td>{e.entityType}</td>
                <td style={{ fontFamily: 'monospace', fontSize: '0.8em' }}>{e.entityId}</td>
                <td>{e.detail}</td>
              </tr>
            ))}
          </tbody>
        </table>
        {entries.length === 0 && <p className="muted">No audit log entries yet.</p>}
        <div className="row" style={{ marginTop: '1rem' }}>
          {hasMore && (
            <button type="button" className="secondary" onClick={() => load(filter, false)}>
              Load more
            </button>
          )}
        </div>
        <p className="muted">
          Records every mutating admin/API action: users, maps, map permissions and aliases, version
          uploads, geo objects, API keys, and sync remotes. Most recent first.
        </p>
      </div>
    </div>
  );
}
