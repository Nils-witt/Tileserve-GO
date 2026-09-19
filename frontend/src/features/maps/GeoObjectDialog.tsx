import { useEffect, useState, type ChangeEvent, type FormEvent } from 'react';
import { Button, Stack, TextField } from '@mui/material';
import type { GeoObject, MapSummary } from '../../api/types';
import ErrorBanner from '../../components/ErrorBanner';
import Modal from '../../components/Modal';
import PreviewModal from './PreviewModal';

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

type GeoObjectForm = typeof emptyForm;

function formFromObject(geoObject?: GeoObject): GeoObjectForm {
  if (!geoObject) return emptyForm;
  return {
    name: geoObject.name,
    externalId: geoObject.externalId ?? '',
    latitude: String(geoObject.latitude),
    longitude: String(geoObject.longitude),
    street: geoObject.street ?? '',
    housenumber: geoObject.housenumber ?? '',
    postcode: geoObject.postcode ?? '',
    city: geoObject.city ?? '',
    cityDistrict: geoObject.cityDistrict ?? '',
  };
}

export default function GeoObjectDialog({
  open,
  map,
  version,
  geoObject,
  onClose,
  onSubmit,
}: {
  open: boolean;
  /** Map and tileset version the picker preview should display tiles for. */
  map: MapSummary;
  version: string;
  /** Omitted for "add", present for "edit". */
  geoObject?: GeoObject;
  onClose: () => void;
  onSubmit: (payload: Partial<GeoObject>) => Promise<void>;
}) {
  const [form, setForm] = useState<GeoObjectForm>(() => formFromObject(geoObject));
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [pickerOpen, setPickerOpen] = useState(false);

  useEffect(() => {
    if (open) {
      setForm(formFromObject(geoObject));
      setError(null);
    }
    // Only reset when the dialog opens, not on every geoObject identity change.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  const set = (key: keyof GeoObjectForm) => (e: ChangeEvent<HTMLInputElement>) =>
    setForm((f) => ({ ...f, [key]: e.target.value }));

  const handlePick = (latitude: number, longitude: number) => {
    setForm((f) => ({ ...f, latitude: String(latitude), longitude: String(longitude) }));
  };

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault();
    setError(null);
    setSubmitting(true);
    try {
      await onSubmit({
        name: form.name,
        externalId: form.externalId,
        latitude: parseFloat(form.latitude),
        longitude: parseFloat(form.longitude),
        street: form.street,
        housenumber: form.housenumber,
        postcode: form.postcode,
        city: form.city,
        cityDistrict: form.cityDistrict,
      });
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Modal open={open} title={geoObject ? 'Edit geo object' : 'Add geo object'} onClose={onClose}>
      <Stack component="form" spacing={2} sx={{ pt: 1 }} onSubmit={handleSubmit}>
        <ErrorBanner message={error} />
        <TextField label="Name" size="small" required value={form.name} onChange={set('name')} />
        <TextField
          label="External ID"
          size="small"
          value={form.externalId}
          onChange={set('externalId')}
        />
        <Stack direction="row" spacing={2}>
          <TextField
            label="Latitude"
            size="small"
            type="number"
            required
            fullWidth
            slotProps={{ htmlInput: { step: 'any' } }}
            value={form.latitude}
            onChange={set('latitude')}
          />
          <TextField
            label="Longitude"
            size="small"
            type="number"
            required
            fullWidth
            slotProps={{ htmlInput: { step: 'any' } }}
            value={form.longitude}
            onChange={set('longitude')}
          />
        </Stack>
        <Button size="small" onClick={() => setPickerOpen(true)} sx={{ alignSelf: 'flex-start' }}>
          Pick on map
        </Button>
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
        <Stack direction="row" spacing={1} sx={{ justifyContent: 'flex-end' }}>
          <Button onClick={onClose} disabled={submitting}>
            Cancel
          </Button>
          <Button type="submit" variant="contained" disabled={submitting}>
            {geoObject ? 'Save' : 'Add'}
          </Button>
        </Stack>
      </Stack>
      <PreviewModal
        map={pickerOpen ? map : null}
        version={version}
        pickable
        initialLatitude={Number.isFinite(parseFloat(form.latitude)) ? parseFloat(form.latitude) : undefined}
        initialLongitude={
          Number.isFinite(parseFloat(form.longitude)) ? parseFloat(form.longitude) : undefined
        }
        onPick={handlePick}
        onClose={() => setPickerOpen(false)}
      />
    </Modal>
  );
}
