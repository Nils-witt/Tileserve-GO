import { useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import {
  Button,
  Checkbox,
  FormControl,
  InputLabel,
  MenuItem,
  Paper,
  Select,
  Stack,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  Typography,
} from '@mui/material';
import type { GeoObject, MapSummary } from '../../api/types';
import { useAdminData } from '../AdminDataContext';
import ErrorBanner from '../../components/ErrorBanner';
import { useGeoObjects } from './useGeoObjects';
import GeoObjectRow from './GeoObjectRow';
import GeoObjectDialog from './GeoObjectDialog';

function GeoObjectsPageContent({ map }: { map: MapSummary }) {
  const {
    versions,
    version,
    setVersion,
    objects,
    selected,
    error,
    toggleSelect,
    toggleSelectAll,
    saveObject,
    createObject,
    deleteObject,
    deleteSelected,
  } = useGeoObjects(map);

  const [editing, setEditing] = useState<GeoObject | 'new' | null>(null);

  const submitDialog = (payload: Partial<GeoObject>) =>
    editing && editing !== 'new' ? saveObject(editing.uuid, payload) : createObject(payload);

  return (
    <Paper sx={{ p: 3 }}>
      <Stack direction="row" sx={{ alignItems: 'center', justifyContent: 'space-between', mb: 2 }}>
        <Typography variant="h6" component="h2">
          Geo objects — {map.name}
        </Typography>
        <Button component={Link} to="/ui/maps" size="small">
          Back to maps
        </Button>
      </Stack>
      <ErrorBanner message={error} />
      <Stack direction="row" spacing={2} sx={{ mb: 2 }}>
        <FormControl size="small" sx={{ minWidth: 140 }}>
          <InputLabel id="geo-version-label">Version</InputLabel>
          <Select
            labelId="geo-version-label"
            label="Version"
            value={version}
            onChange={(e) => setVersion(e.target.value)}
          >
            {versions.map((v) => (
              <MenuItem key={v} value={v}>
                v{v}
              </MenuItem>
            ))}
          </Select>
        </FormControl>
      </Stack>
      <Stack direction="row" spacing={1} sx={{ mb: 2 }}>
        <Button variant="contained" size="small" onClick={() => setEditing('new')}>
          Add geo object
        </Button>
        <Button
          variant="contained"
          color="error"
          size="small"
          disabled={selected.size === 0}
          onClick={deleteSelected}
        >
          Delete selected ({selected.size})
        </Button>
      </Stack>
      <TableContainer>
        <Table size="small">
          <TableHead>
            <TableRow>
              <TableCell align="center" padding="checkbox">
                <Checkbox
                  checked={objects.length > 0 && selected.size === objects.length}
                  onChange={(e) => toggleSelectAll(e.target.checked)}
                />
              </TableCell>
              <TableCell>Name</TableCell>
              <TableCell>External ID</TableCell>
              <TableCell>Latitude</TableCell>
              <TableCell>Longitude</TableCell>
              <TableCell>Street</TableCell>
              <TableCell>House no.</TableCell>
              <TableCell>Postcode</TableCell>
              <TableCell>City</TableCell>
              <TableCell>City district</TableCell>
              <TableCell>Actions</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {objects.map((o) => (
              <GeoObjectRow
                key={o.uuid}
                obj={o}
                selected={selected.has(o.uuid)}
                onToggleSelect={() => toggleSelect(o.uuid)}
                onEdit={() => setEditing(o)}
                onDelete={() => deleteObject(o.uuid)}
              />
            ))}
          </TableBody>
        </Table>
      </TableContainer>
      {objects.length === 0 && (
        <Typography variant="body2" color="text.secondary" sx={{ mt: 1 }}>
          No geo objects for this version yet.
        </Typography>
      )}

      <GeoObjectDialog
        open={editing !== null}
        map={map}
        version={version}
        geoObject={editing && editing !== 'new' ? editing : undefined}
        onClose={() => setEditing(null)}
        onSubmit={submitDialog}
      />
    </Paper>
  );
}

export default function GeoObjectsPage() {
  const { uuid } = useParams<{ uuid: string }>();
  const { maps } = useAdminData();
  const map = maps.find((m) => m.uuid === uuid);

  if (!map) {
    return (
      <Paper sx={{ p: 3 }}>
        <Typography variant="body2" color="text.secondary">
          {maps.length === 0 ? 'Loading…' : 'Map not found.'}
        </Typography>
        <Button component={Link} to="/ui/maps" size="small" sx={{ mt: 2 }}>
          Back to maps
        </Button>
      </Paper>
    );
  }

  return <GeoObjectsPageContent key={map.uuid} map={map} />;
}
