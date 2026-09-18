import { useState } from 'react';
import { Link } from 'react-router-dom';
import {
  Box,
  Button,
  Checkbox,
  Paper,
  Stack,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  Typography,
} from '@mui/material';
import { apiFetch } from '../api/client.ts';
import type { MapSummary } from '../api/types.ts';
import { useAdminData } from '../features/AdminDataContext.tsx';
import ErrorBanner from '../components/ErrorBanner.tsx';
import { fmtDate } from '../lib/format.ts';
import PreviewModal from '../features/maps/PreviewModal.tsx';
import MapFormModal from '../features/maps/MapFormModal.tsx';
import VersionsModal from '../features/maps/VersionsModal.tsx';

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
  const [versionsOpen, setVersionsOpen] = useState(false);
  const [previewOpen, setPreviewOpen] = useState(false);
  const [editOpen, setEditOpen] = useState(false);

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

  return (
    <>
      <TableRow>
        <TableCell>
          {m.name}
          <br />
          <Typography variant="caption" color="text.secondary">
            {m.uuid}
          </Typography>
        </TableCell>
        <TableCell>{m.currentVersion || '-'}</TableCell>
        <TableCell align="center" padding="checkbox">
          <Checkbox checked={m.visibleToAll} title="Visible to all users" disabled={true} />
        </TableCell>
        <TableCell align="center" padding="checkbox">
          <Checkbox
            checked={m.anonymousAllowed}
            title="Allow fetching tile files without signing in"
            disabled={true}
          />
        </TableCell>
        <TableCell>{m.owner}</TableCell>
        <TableCell>
          {fmtDate(m.createdAt)}
          <br />
          <Typography variant="caption" color="text.secondary">
            by {m.createdBy}
          </Typography>
        </TableCell>
        <TableCell>
          {fmtDate(m.updatedAt)}
          <br />
          <Typography variant="caption" color="text.secondary">
            by {m.updatedBy}
          </Typography>
        </TableCell>
        <TableCell sx={{ minWidth: 260 }}>
          <Stack direction="row" spacing={1} useFlexGap sx={{ flexWrap: 'wrap' }}>
            <Button size="small" onClick={() => setEditOpen(true)}>
              Edit
            </Button>
            <Button size="small" onClick={() => setVersionsOpen(true)}>
              Versions
            </Button>
            <Button
              size="small"
              disabled={!m.currentVersion}
              title={!m.currentVersion ? 'No uploaded version yet' : undefined}
              onClick={() => setPreviewOpen(true)}
            >
              Preview
            </Button>
            <Button
              size="small"
              disabled={!m.currentVersion}
              title={!m.currentVersion ? 'No uploaded version yet' : undefined}
              component={Link}
              to={`/ui/maps/${m.uuid}/geo-objects`}
            >
              Geo objects
            </Button>
            <Button size="small" color="error" onClick={deleteMap}>
              Delete
            </Button>
          </Stack>
          <ErrorBanner message={error} />
        </TableCell>
      </TableRow>
      <VersionsModal
        map={versionsOpen ? m : null}
        isAdmin={isAdmin}
        onClose={() => setVersionsOpen(false)}
        onUploaded={onReload}
      />
      <PreviewModal map={previewOpen ? m : null} onClose={() => setPreviewOpen(false)} />
      <MapFormModal open={editOpen} map={m} onClose={() => setEditOpen(false)} onSaved={onReload} />
    </>
  );
}

export default function MapsListPage() {
  const { maps, isAdmin, reloadMaps } = useAdminData();
  const [createOpen, setCreateOpen] = useState(false);

  return (
    <Box>
      <Paper sx={{ p: 3 }}>
        <Stack
          direction="row"
          sx={{ alignItems: 'center', justifyContent: 'space-between', mb: 2 }}
        >
          <Typography variant="h6" component="h2">
            Maps
          </Typography>
          <Button variant="contained" onClick={() => setCreateOpen(true)}>
            Create map
          </Button>
        </Stack>
        <TableContainer>
          <Table size="small">
            <TableHead>
              <TableRow>
                <TableCell>Name</TableCell>
                <TableCell>Version</TableCell>
                <TableCell align="center">Public</TableCell>
                <TableCell align="center">Anonymous</TableCell>
                <TableCell>Owner</TableCell>
                <TableCell>Created</TableCell>
                <TableCell>Updated</TableCell>
                <TableCell>Actions</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {maps.map((m) => (
                <MapRow key={m.uuid} m={m} isAdmin={isAdmin} onReload={reloadMaps} />
              ))}
            </TableBody>
          </Table>
        </TableContainer>
        {maps.length === 0 && (
          <Typography variant="body2" color="text.secondary" sx={{ mt: 2 }}>
            No maps yet.
          </Typography>
        )}
        <Typography variant="body2" color="text.secondary" sx={{ mt: 2 }}>
          Maps are private by default — only admins, the owner, and users granted access under
          "Permissions" can see them, unless "Public" is checked. "Anonymous" separately controls
          whether tile files (<code>/maps/&lt;uuid&gt;/version/&lt;version&gt;/...</code>) can be
          fetched without signing in at all, regardless of "Public".
        </Typography>
      </Paper>
      <MapFormModal
        open={createOpen}
        map={null}
        onClose={() => setCreateOpen(false)}
        onSaved={reloadMaps}
      />
    </Box>
  );
}
