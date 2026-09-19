import { useState, type FormEvent } from 'react';
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
import ErrorBanner from '../../components/ErrorBanner';
import { fmtDate } from '../../lib/format';
import {
  AuditLogProvider,
  emptyAuditLogFilter,
  useAuditLog,
  type AuditLogFilter,
} from './AuditLogContext';

function AuditTabContent() {
  const { entries, hasMore, error, load } = useAuditLog();
  const [filter, setFilter] = useState<AuditLogFilter>(emptyAuditLogFilter);

  const handleSubmit = (e: FormEvent) => {
    e.preventDefault();
    load(filter, true);
  };

  const handleClear = () => {
    setFilter(emptyAuditLogFilter);
    load(emptyAuditLogFilter, true);
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

export default function AuditTab() {
  return (
    <AuditLogProvider>
      <AuditTabContent />
    </AuditLogProvider>
  );
}
