import { useCallback, useEffect, useState, type ChangeEvent, type FormEvent } from 'react';
import { Link, useParams } from 'react-router-dom';
import {
  Box,
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
  TextField,
  Typography,
} from '@mui/material';
import { apiFetch, apiJson } from '../../api/client';
import type { GeoObject, MapSummary, MapVersion } from '../../api/types';
import { useAdminData } from '../AdminDataContext';
import ErrorBanner from '../../components/ErrorBanner';

const emptyForm = {
  name: '',
  externalId: '',
  latitude: '',
  longitude: '',
  street: '',
  housenumber: '',
  postcode: '',
  city: '',
  cityDistrict: '',
};

const inlineFieldSx = { width: '9rem' };

function GeoObjectRow({
  obj,
  selected,
  onToggleSelect,
  onSave,
  onDelete,
}: {
  obj: GeoObject;
  selected: boolean;
  onToggleSelect: () => void;
  onSave: (payload: Partial<GeoObject>) => void;
  onDelete: () => void;
}) {
  const [form, setForm] = useState({
    name: obj.name,
    externalId: obj.externalId ?? '',
    latitude: String(obj.latitude),
    longitude: String(obj.longitude),
    street: obj.street ?? '',
    housenumber: obj.housenumber ?? '',
    postcode: obj.postcode ?? '',
    city: obj.city ?? '',
    cityDistrict: obj.cityDistrict ?? '',
  });

  const set = (key: keyof typeof form) => (e: ChangeEvent<HTMLInputElement>) =>
    setForm((f) => ({ ...f, [key]: e.target.value }));

  return (
    <TableRow>
      <TableCell align="center" padding="checkbox">
        <Checkbox checked={selected} onChange={onToggleSelect} />
      </TableCell>
      <TableCell>
        <TextField size="small" sx={inlineFieldSx} value={form.name} onChange={set('name')} />
      </TableCell>
      <TableCell>
        <TextField
          size="small"
          sx={inlineFieldSx}
          value={form.externalId}
          onChange={set('externalId')}
        />
      </TableCell>
      <TableCell>
        <TextField
          size="small"
          type="number"
          sx={inlineFieldSx}
          slotProps={{ htmlInput: { step: 'any' } }}
          value={form.latitude}
          onChange={set('latitude')}
        />
      </TableCell>
      <TableCell>
        <TextField
          size="small"
          type="number"
          sx={inlineFieldSx}
          slotProps={{ htmlInput: { step: 'any' } }}
          value={form.longitude}
          onChange={set('longitude')}
        />
      </TableCell>
      <TableCell>
        <TextField size="small" sx={inlineFieldSx} value={form.street} onChange={set('street')} />
      </TableCell>
      <TableCell>
        <TextField
          size="small"
          sx={inlineFieldSx}
          value={form.housenumber}
          onChange={set('housenumber')}
        />
      </TableCell>
      <TableCell>
        <TextField
          size="small"
          sx={inlineFieldSx}
          value={form.postcode}
          onChange={set('postcode')}
        />
      </TableCell>
      <TableCell>
        <TextField size="small" sx={inlineFieldSx} value={form.city} onChange={set('city')} />
      </TableCell>
      <TableCell>
        <TextField
          size="small"
          sx={inlineFieldSx}
          value={form.cityDistrict}
          onChange={set('cityDistrict')}
        />
      </TableCell>
      <TableCell sx={{ minWidth: 140 }}>
        <Stack direction="row" spacing={1}>
          <Button
            size="small"
            onClick={() =>
              onSave({
                name: form.name,
                externalId: form.externalId,
                latitude: parseFloat(form.latitude),
                longitude: parseFloat(form.longitude),
                street: form.street,
                housenumber: form.housenumber,
                postcode: form.postcode,
                city: form.city,
                cityDistrict: form.cityDistrict,
              })
            }
          >
            Save
          </Button>
          <Button size="small" color="error" onClick={onDelete}>
            Delete
          </Button>
        </Stack>
      </TableCell>
    </TableRow>
  );
}

function GeoObjectsPageContent({ map }: { map: MapSummary }) {
  const [versions, setVersions] = useState<string[]>([]);
  const [version, setVersion] = useState('');
  const [objects, setObjects] = useState<GeoObject[]>([]);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [error, setError] = useState<string | null>(null);
  const [form, setForm] = useState(emptyForm);

  const path = useCallback(
    (suffix: string) =>
      `/maps/${map.uuid}/version/${encodeURIComponent(version)}/geo-objects${suffix}`,
    [map, version],
  );

  const loadObjects = useCallback(async () => {
    if (!version) return;
    setError(null);
    try {
      setObjects(await apiJson<GeoObject[]>(path('')));
      setSelected(new Set());
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }, [version, path]);

  useEffect(() => {
    setError(null);
    setForm(emptyForm);
    (async () => {
      try {
        const vs = await apiJson<MapVersion[]>(`/maps/${map.uuid}/versions`);
        const values = vs.map((v) => v.version);
        if (!values.includes(map.currentVersion)) values.unshift(map.currentVersion);
        setVersions(values);
        setVersion(map.currentVersion);
      } catch (err) {
        setError(err instanceof Error ? err.message : String(err));
        setVersions([map.currentVersion]);
        setVersion(map.currentVersion);
      }
    })();
    // Only re-run when a different map is opened.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [map]);

  useEffect(() => {
    loadObjects();
  }, [loadObjects]);

  const toggleSelect = (id: string) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  const toggleSelectAll = (checked: boolean) => {
    setSelected(checked ? new Set(objects.map((o) => o.uuid)) : new Set());
  };

  const saveObject = async (id: string, payload: Partial<GeoObject>) => {
    setError(null);
    try {
      await apiFetch(path('/' + id), {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      });
      await loadObjects();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  const deleteObject = async (id: string) => {
    if (!confirm('Delete this geo object?')) return;
    setError(null);
    try {
      await apiFetch(path('/' + id), { method: 'DELETE' });
      await loadObjects();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  const deleteSelected = async () => {
    const ids = Array.from(selected);
    if (ids.length === 0) return;
    if (!confirm(`Delete ${ids.length} selected geo object(s)?`)) return;
    setError(null);
    try {
      for (const id of ids) {
        await apiFetch(path('/' + id), { method: 'DELETE' });
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      await loadObjects();
    }
  };

  const createObject = async (e: FormEvent) => {
    e.preventDefault();
    setError(null);
    try {
      await apiFetch(path(''), {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          name: form.name,
          externalId: form.externalId,
          latitude: parseFloat(form.latitude),
          longitude: parseFloat(form.longitude),
          street: form.street,
          housenumber: form.housenumber,
          postcode: form.postcode,
          city: form.city,
          cityDistrict: form.cityDistrict,
        }),
      });
      setForm(emptyForm);
      await loadObjects();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  const set = (key: keyof typeof form) => (e: ChangeEvent<HTMLInputElement>) =>
    setForm((f) => ({ ...f, [key]: e.target.value }));

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
      <Box sx={{ mb: 2 }}>
        <Button
          variant="contained"
          color="error"
          size="small"
          disabled={selected.size === 0}
          onClick={deleteSelected}
        >
          Delete selected ({selected.size})
        </Button>
      </Box>
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
                onSave={(payload) => saveObject(o.uuid, payload)}
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

      <Typography variant="h6" component="h2" sx={{ mt: 3, mb: 1 }}>
        Add geo object
      </Typography>
      <Stack
        component="form"
        direction="row"
        spacing={2}
        useFlexGap
        sx={{ flexWrap: 'wrap' }}
        onSubmit={createObject}
      >
        <TextField label="Name" size="small" required value={form.name} onChange={set('name')} />
        <TextField
          label="External ID"
          size="small"
          value={form.externalId}
          onChange={set('externalId')}
        />
        <TextField
          label="Latitude"
          size="small"
          type="number"
          required
          slotProps={{ htmlInput: { step: 'any' } }}
          value={form.latitude}
          onChange={set('latitude')}
        />
        <TextField
          label="Longitude"
          size="small"
          type="number"
          required
          slotProps={{ htmlInput: { step: 'any' } }}
          value={form.longitude}
          onChange={set('longitude')}
        />
        <TextField label="Street" size="small" value={form.street} onChange={set('street')} />
        <TextField
          label="House no."
          size="small"
          value={form.housenumber}
          onChange={set('housenumber')}
        />
        <TextField label="Postcode" size="small" value={form.postcode} onChange={set('postcode')} />
        <TextField label="City" size="small" value={form.city} onChange={set('city')} />
        <TextField
          label="City district"
          size="small"
          value={form.cityDistrict}
          onChange={set('cityDistrict')}
        />
        <Button type="submit" variant="contained">
          Add
        </Button>
      </Stack>
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
