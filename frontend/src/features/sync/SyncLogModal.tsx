import { Box, Button, Typography } from '@mui/material';
import type { SyncRemote } from '../../api/types';
import Modal from '../../components/Modal';
import ErrorBanner from '../../components/ErrorBanner';
import { fmtDate } from '../../lib/format';
import { SyncLogProvider, useSyncLog } from './SyncLogContext';

function SyncLogModalContent({
  remote,
  onClose,
}: {
  remote: SyncRemote | null;
  onClose: () => void;
}) {
  const { entries, error, reloadLog } = useSyncLog();

  // Newest entry first, since that's the one an admin checking on a sync is
  // almost always looking for. Error-level entries are highlighted so a
  // failure stands out among routine lines.
  const lines = entries
    .slice()
    .reverse()
    .map((e, i) => {
      const line = `[${fmtDate(e.time)}] ${e.message}`;
      return (
        <Box
          key={i}
          component="span"
          sx={
            e.level === 'error' ? { color: 'error.main', display: 'block' } : { display: 'block' }
          }
        >
          {line}
        </Box>
      );
    });

  return (
    <Modal
      open={!!remote}
      title={remote ? `Sync log — ${remote.name}` : 'Sync log'}
      onClose={onClose}
      headerExtra={
        <Button size="small" onClick={reloadLog}>
          Refresh
        </Button>
      }
    >
      <ErrorBanner message={error} />
      <Box
        component="pre"
        sx={{ whiteSpace: 'pre-wrap', fontFamily: 'monospace', fontSize: '0.8em', m: 0 }}
      >
        {lines}
      </Box>
      {entries.length === 0 && (
        <Typography variant="body2" color="text.secondary">
          No log entries yet.
        </Typography>
      )}
    </Modal>
  );
}

export default function SyncLogModal({
  remote,
  onClose,
}: {
  remote: SyncRemote | null;
  onClose: () => void;
}) {
  return (
    <SyncLogProvider remote={remote}>
      <SyncLogModalContent remote={remote} onClose={onClose} />
    </SyncLogProvider>
  );
}
