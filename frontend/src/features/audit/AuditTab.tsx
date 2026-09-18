import { useEffect, useState, type FormEvent } from 'react';
import {
  Box,
  Button,
  Paper,
  Stack,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  TextField,
  Typography,
} from '@mui/material';
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
    <Box>
      <Paper sx={{ p: 3, mb: 3 }}>
        <Stack
          component="form"
          direction="row"
          spacing={2}
          useFlexGap
          sx={{ flexWrap: 'wrap', alignItems: 'center' }}
          onSubmit={handleSubmit}
        >
          <TextField
            label="Actor"
            size="small"
            placeholder="username"
            value={filter.actor}
            onChange={(e) => setFilter((f) => ({ ...f, actor: e.target.value }))}
          />
          <TextField
            label="Action"
            size="small"
            placeholder="e.g. create"
            value={filter.action}
            onChange={(e) => setFilter((f) => ({ ...f, action: e.target.value }))}
          />
          <TextField
            label="Entity type"
            size="small"
            placeholder="e.g. map"
            value={filter.entityType}
            onChange={(e) => setFilter((f) => ({ ...f, entityType: e.target.value }))}
          />
          <TextField
            label="Entity ID"
            size="small"
            value={filter.entityId}
            onChange={(e) => setFilter((f) => ({ ...f, entityId: e.target.value }))}
          />
          <Button type="submit" variant="contained">
            Filter
          </Button>
          <Button type="button" onClick={handleClear}>
            Clear
          </Button>
        </Stack>
      </Paper>

      <Paper sx={{ p: 3 }}>
        <ErrorBanner message={error} />
        <TableContainer>
          <Table size="small">
            <TableHead>
              <TableRow>
                <TableCell>Time</TableCell>
                <TableCell>Actor</TableCell>
                <TableCell>Action</TableCell>
                <TableCell>Entity type</TableCell>
                <TableCell>Entity ID</TableCell>
                <TableCell>Detail</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {entries.map((e, i) => (
                <TableRow key={i}>
                  <TableCell>{fmtDate(e.occurredAt)}</TableCell>
                  <TableCell>{e.actor}</TableCell>
                  <TableCell>{e.action}</TableCell>
                  <TableCell>{e.entityType}</TableCell>
                  <TableCell sx={{ fontFamily: 'monospace', fontSize: '0.8em' }}>
                    {e.entityId}
                  </TableCell>
                  <TableCell>{e.detail}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </TableContainer>
        {entries.length === 0 && (
          <Typography variant="body2" color="text.secondary" sx={{ mt: 2 }}>
            No audit log entries yet.
          </Typography>
        )}
        <Box sx={{ mt: 2 }}>
          {hasMore && <Button onClick={() => load(filter, false)}>Load more</Button>}
        </Box>
        <Typography variant="body2" color="text.secondary" sx={{ mt: 2 }}>
          Records every mutating admin/API action: users, maps, map permissions and aliases, version
          uploads, geo objects, API keys, and sync remotes. Most recent first.
        </Typography>
      </Paper>
    </Box>
  );
}
