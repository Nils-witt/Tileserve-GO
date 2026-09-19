import { useState } from 'react';
import {
  Button,
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
import { api } from '../../api/ApiClient';
import type { MapSummary } from '../../api/types';
import ErrorBanner from '../../components/ErrorBanner';
import { fmtDate } from '../../lib/format';
import { AliasesProvider, useAliases } from './AliasesContext';

function AliasesPanelContent({ map }: { map: MapSummary }) {
  const { aliases, error: loadError, reloadAliases } = useAliases();
  const [error, setError] = useState<string | null>(null);
  const [name, setName] = useState('');
  const [version, setVersion] = useState('');

  const save = async () => {
    if (!name.trim() || !version.trim()) return;
    setError(null);
    try {
      await api.setMapAlias(map.uuid, name.trim(), version.trim());
      setName('');
      setVersion('');
      await reloadAliases();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  const remove = async (alias: string) => {
    setError(null);
    try {
      await api.deleteMapAlias(map.uuid, alias);
      await reloadAliases();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  return (
    <>
      <ErrorBanner message={error ?? loadError} />
      <TableContainer>
        <Table size="small">
          <TableHead>
            <TableRow>
              <TableCell>Alias</TableCell>
              <TableCell>Version</TableCell>
              <TableCell>Updated</TableCell>
              <TableCell>Actions</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {aliases.map((a) => (
              <TableRow key={a.alias}>
                <TableCell>{a.alias}</TableCell>
                <TableCell>{a.version}</TableCell>
                <TableCell>
                  {fmtDate(a.updatedAt)}
                  <br />
                  <Typography variant="caption" color="text.secondary">
                    by {a.updatedBy}
                  </Typography>
                </TableCell>
                <TableCell>
                  <Button size="small" color="error" onClick={() => remove(a.alias)}>
                    Delete
                  </Button>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </TableContainer>
      {aliases.length === 0 && (
        <Typography variant="body2" color="text.secondary" sx={{ mt: 1 }}>
          No aliases yet.
        </Typography>
      )}
      <Stack
        direction="row"
        spacing={2}
        useFlexGap
        sx={{ flexWrap: 'wrap', alignItems: 'flex-end', mt: 2 }}
      >
        <TextField
          label="Alias name"
          size="small"
          placeholder="e.g. stable"
          value={name}
          onChange={(e) => setName(e.target.value)}
        />
        <TextField
          label="Version"
          size="small"
          placeholder="e.g. 7"
          value={version}
          onChange={(e) => setVersion(e.target.value)}
        />
        <Button variant="contained" onClick={save}>
          Save
        </Button>
      </Stack>
    </>
  );
}

export default function AliasesPanel({ map }: { map: MapSummary }) {
  return (
    <AliasesProvider map={map}>
      <AliasesPanelContent map={map} />
    </AliasesProvider>
  );
}
