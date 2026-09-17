import { useCallback, useEffect, useState, type ChangeEvent, type FormEvent } from 'react';
import { apiFetch, apiJson } from '../../api/client';
import type { GeoObject, MapSummary, MapVersion } from '../../api/types';
import Modal from '../../components/Modal';
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
    <tr>
      <td className="checkbox-cell">
        <input
          type="checkbox"
          className="geo-select"
          checked={selected}
          onChange={onToggleSelect}
        />
      </td>
      <td>
        <input className="inline" value={form.name} onChange={set('name')} />
      </td>
      <td>
        <input className="inline" value={form.externalId} onChange={set('externalId')} />
      </td>
      <td>
        <input
          className="inline"
          type="number"
          step="any"
          value={form.latitude}
          onChange={set('latitude')}
        />
      </td>
      <td>
        <input
          className="inline"
          type="number"
          step="any"
          value={form.longitude}
          onChange={set('longitude')}
        />
      </td>
      <td>
        <input className="inline" value={form.street} onChange={set('street')} />
      </td>
      <td>
        <input className="inline" value={form.housenumber} onChange={set('housenumber')} />
      </td>
      <td>
        <input className="inline" value={form.postcode} onChange={set('postcode')} />
      </td>
      <td>
        <input className="inline" value={form.city} onChange={set('city')} />
      </td>
      <td>
        <input className="inline" value={form.cityDistrict} onChange={set('cityDistrict')} />
      </td>
      <td>
        <div className="actions">
          <button
            type="button"
            className="secondary"
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
          </button>
          <button type="button" className="danger" onClick={onDelete}>
            Delete
          </button>
        </div>
      </td>
    </tr>
  );
}

export default function GeoObjectsModal({
  map,
  onClose,
}: {
  map: MapSummary | null;
  onClose: () => void;
}) {
  const [versions, setVersions] = useState<string[]>([]);
  const [version, setVersion] = useState('');
  const [objects, setObjects] = useState<GeoObject[]>([]);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [error, setError] = useState<string | null>(null);
  const [form, setForm] = useState(emptyForm);

  const path = useCallback(
    (suffix: string) =>
      `/maps/${map!.uuid}/version/${encodeURIComponent(version)}/geo-objects${suffix}`,
    [map, version],
  );

  const loadObjects = useCallback(async () => {
    if (!map || !version) return;
    setError(null);
    try {
      setObjects(await apiJson<GeoObject[]>(path('')));
      setSelected(new Set());
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }, [map, version, path]);

  useEffect(() => {
    if (!map) return;
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
    <Modal open={!!map} title={map ? `Geo objects — ${map.name}` : 'Geo objects'} onClose={onClose}>
      <ErrorBanner message={error} />
      <div className="row">
        <div className="field">
          <label htmlFor="geo-version">Version</label>
          <select id="geo-version" value={version} onChange={(e) => setVersion(e.target.value)}>
            {versions.map((v) => (
              <option key={v} value={v}>
                v{v}
              </option>
            ))}
          </select>
        </div>
      </div>
      <div className="row" style={{ alignItems: 'center' }}>
        <button
          type="button"
          className="danger"
          disabled={selected.size === 0}
          onClick={deleteSelected}
        >
          Delete selected ({selected.size})
        </button>
      </div>
      <table>
        <thead>
          <tr>
            <th className="checkbox-cell">
              <input
                type="checkbox"
                checked={objects.length > 0 && selected.size === objects.length}
                onChange={(e) => toggleSelectAll(e.target.checked)}
              />
            </th>
            <th>Name</th>
            <th>External ID</th>
            <th>Latitude</th>
            <th>Longitude</th>
            <th>Street</th>
            <th>House no.</th>
            <th>Postcode</th>
            <th>City</th>
            <th>City district</th>
            <th>Actions</th>
          </tr>
        </thead>
        <tbody>
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
        </tbody>
      </table>
      {objects.length === 0 && <p className="muted">No geo objects for this version yet.</p>}

      <h2 style={{ marginTop: '1.25rem' }}>Add geo object</h2>
      <form className="row" onSubmit={createObject}>
        <div className="field">
          <label htmlFor="geo-create-name">Name</label>
          <input id="geo-create-name" required value={form.name} onChange={set('name')} />
        </div>
        <div className="field">
          <label htmlFor="geo-create-external">External ID</label>
          <input id="geo-create-external" value={form.externalId} onChange={set('externalId')} />
        </div>
        <div className="field">
          <label htmlFor="geo-create-lat">Latitude</label>
          <input
            id="geo-create-lat"
            type="number"
            step="any"
            required
            value={form.latitude}
            onChange={set('latitude')}
          />
        </div>
        <div className="field">
          <label htmlFor="geo-create-lon">Longitude</label>
          <input
            id="geo-create-lon"
            type="number"
            step="any"
            required
            value={form.longitude}
            onChange={set('longitude')}
          />
        </div>
        <div className="field">
          <label htmlFor="geo-create-street">Street</label>
          <input id="geo-create-street" value={form.street} onChange={set('street')} />
        </div>
        <div className="field">
          <label htmlFor="geo-create-housenumber">House no.</label>
          <input
            id="geo-create-housenumber"
            value={form.housenumber}
            onChange={set('housenumber')}
          />
        </div>
        <div className="field">
          <label htmlFor="geo-create-postcode">Postcode</label>
          <input id="geo-create-postcode" value={form.postcode} onChange={set('postcode')} />
        </div>
        <div className="field">
          <label htmlFor="geo-create-city">City</label>
          <input id="geo-create-city" value={form.city} onChange={set('city')} />
        </div>
        <div className="field">
          <label htmlFor="geo-create-city-district">City district</label>
          <input
            id="geo-create-city-district"
            value={form.cityDistrict}
            onChange={set('cityDistrict')}
          />
        </div>
        <button type="submit">Add</button>
      </form>
    </Modal>
  );
}
