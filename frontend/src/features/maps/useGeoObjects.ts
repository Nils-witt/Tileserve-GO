import { useCallback, useEffect, useState } from 'react';
import { apiFetch, apiJson } from '../../api/client';
import type { GeoObject, MapSummary, MapVersion } from '../../api/types';

export function useGeoObjects(map: MapSummary) {
  const [versions, setVersions] = useState<string[]>([]);
  const [version, setVersion] = useState('');
  const [objects, setObjects] = useState<GeoObject[]>([]);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [error, setError] = useState<string | null>(null);

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

  // Left to throw: callers with a form dialog display the error inline and
  // keep the dialog open on failure instead of surfacing it on the page.
  const saveObject = async (id: string, payload: Partial<GeoObject>) => {
    await apiFetch(path('/' + id), {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    });
    await loadObjects();
  };

  const createObject = async (payload: Partial<GeoObject>) => {
    await apiFetch(path(''), {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    });
    await loadObjects();
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

  return {
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
  };
}
