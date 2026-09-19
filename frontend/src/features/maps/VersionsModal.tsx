import { useState } from 'react';
import {
  Button,
  LinearProgress,
  Stack,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  Typography,
} from '@mui/material';
import { apiFetch } from '../../api/client';
import { formatBytes, uploadMapVersion } from '../../api/upload';
import type { MapSummary } from '../../api/types';
import Modal from '../../components/Modal';
import ErrorBanner from '../../components/ErrorBanner';
import { fmtDate } from '../../lib/format';
import { MapVersionsProvider, useMapVersions } from './MapVersionsContext';

function VersionsModalContent({
  map,
  isAdmin,
  onClose,
  onUploaded,
}: {
  map: MapSummary | null;
  isAdmin: boolean;
  onClose: () => void;
  onUploaded: () => void;
}) {
  const { versions, error: loadError, reloadVersions } = useMapVersions();
  const [error, setError] = useState<string | null>(null);
  const [uploading, setUploading] = useState(false);
  const [progress, setProgress] = useState<{ loaded: number; total: number } | null>(null);

  const handleUpload = async (file: File | undefined) => {
    if (!file || !map) return;
    setError(null);
    setUploading(true);
    setProgress({ loaded: 0, total: 0 });
    const result = await uploadMapVersion(map.uuid, file, setProgress);
    setUploading(false);
    setProgress(null);
    if (!result.ok) {
      if (!result.unauthorized)
        setError(result.errorText || `request failed with status ${result.status}`);
    } else {
      reloadVersions();
      onUploaded();
    }
  };

  const download = async (version: string) => {
    if (!map) return;
    try {
      const res = await apiFetch(
        `/maps/${map.uuid}/version/${encodeURIComponent(version)}/download`,
      );
      const blob = await res.blob();
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `${map.uuid}-v${version}.zip`;
      document.body.appendChild(a);
      a.click();
      a.remove();
      URL.revokeObjectURL(url);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  return (
    <Modal
      open={!!map}
      title={map ? `Versions — ${map.name}` : 'Versions'}
      onClose={onClose}
      headerExtra={
        <>
          <Button
            size="small"
            disabled={uploading}
            onClick={() => document.getElementById('versions-upload-input')?.click()}
          >
            Upload version
          </Button>
          <input
            id="versions-upload-input"
            type="file"
            style={{ display: 'none' }}
            accept=".zip,.tar,.tar.gz,.tgz"
            onChange={(e) => {
              handleUpload(e.target.files?.[0]);
              e.target.value = '';
            }}
          />
        </>
      }
    >
      <ErrorBanner message={error ?? loadError} />
      {progress && (
        <Stack direction="row" spacing={1} sx={{ mb: 2, alignItems: 'center' }}>
          <LinearProgress
            variant={progress.total ? 'determinate' : 'indeterminate'}
            value={progress.total ? (progress.loaded / progress.total) * 100 : undefined}
            sx={{ flex: 1 }}
          />
          <Typography variant="caption" color="text.secondary" sx={{ whiteSpace: 'nowrap' }}>
            {progress.total
              ? `${formatBytes(progress.loaded)} / ${formatBytes(progress.total)}`
              : 'Uploading…'}
          </Typography>
        </Stack>
      )}
      <TableContainer>
        <Table size="small">
          <TableHead>
            <TableRow>
              <TableCell>Version</TableCell>
              <TableCell>Created</TableCell>
              {isAdmin && <TableCell>Actions</TableCell>}
            </TableRow>
          </TableHead>
          <TableBody>
            {versions.map((v) => (
              <TableRow key={v.version}>
                <TableCell>v{v.version}</TableCell>
                <TableCell>
                  {fmtDate(v.createdAt)}
                  <br />
                  <Typography variant="caption" color="text.secondary">
                    by {v.createdBy}
                  </Typography>
                </TableCell>
                {isAdmin && (
                  <TableCell>
                    <Button size="small" onClick={() => download(v.version)}>
                      Download
                    </Button>
                  </TableCell>
                )}
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </TableContainer>
      {versions.length === 0 && (
        <Typography variant="body2" color="text.secondary" sx={{ mt: 1 }}>
          No versions uploaded yet.
        </Typography>
      )}
    </Modal>
  );
}

export default function VersionsModal({
  map,
  isAdmin,
  onClose,
  onUploaded,
}: {
  map: MapSummary | null;
  isAdmin: boolean;
  onClose: () => void;
  onUploaded: () => void;
}) {
  return (
    <MapVersionsProvider map={map}>
      <VersionsModalContent map={map} isAdmin={isAdmin} onClose={onClose} onUploaded={onUploaded} />
    </MapVersionsProvider>
  );
}
