import { useEffect, useState } from 'react';
import { Button, Stack, TextField } from '@mui/material';
import { api } from '../../api/ApiClient';
import type { MapSummary } from '../../api/types';
import ErrorBanner from '../../components/ErrorBanner';

export default function TransferOwnerPanel({
  map,
  onSaved,
}: {
  map: MapSummary;
  onSaved: () => void;
}) {
  const [owner, setOwner] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    setOwner(map.owner);
    setError(null);
  }, [map]);

  const save = async () => {
    if (!owner.trim() || owner.trim() === map.owner) return;
    setError(null);
    setSaving(true);
    try {
      await api.transferMapOwner(map.uuid, owner.trim());
      onSaved();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  };

  return (
    <>
      <ErrorBanner message={error} />
      <Stack
        direction="row"
        spacing={2}
        useFlexGap
        sx={{ flexWrap: 'wrap', alignItems: 'flex-end' }}
      >
        <TextField
          label="New owner username"
          size="small"
          value={owner}
          onChange={(e) => setOwner(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter') save();
          }}
        />
        <Button
          variant="contained"
          disabled={saving || !owner.trim() || owner.trim() === map.owner}
          onClick={save}
        >
          Transfer
        </Button>
      </Stack>
    </>
  );
}
