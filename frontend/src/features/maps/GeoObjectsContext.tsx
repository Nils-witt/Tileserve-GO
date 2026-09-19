import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from 'react';
import { apiFetch, apiJson } from '../../api/client';
import type { GeoObject, MapSummary } from '../../api/types';
import { useMapVersions } from './MapVersionsContext';

interface GeoObjectsData {
  versions: string[];
  version: string;
  setVersion: (v: string) => void;
  objects: GeoObject[];
  selected: Set<string>;
  error: string | null;
  toggleSelect: (id: string) => void;
  toggleSelectAll: (checked: boolean) => void;
  saveObject: (id: string, payload: Partial<GeoObject>) => Promise<void>;
  createObject: (payload: Partial<GeoObject>) => Promise<void>;
  deleteObject: (id: string) => Promise<void>;
  deleteSelected: () => Promise<void>;
}

const GeoObjectsContext = createContext<GeoObjectsData | null>(null);

/** Must be mounted inside a MapVersionsProvider for the same map — it reuses
 * that context's /maps/{uuid}/versions fetch instead of re-fetching it. */
export function GeoObjectsProvider({ map, children }: { map: MapSummary; children: ReactNode }) {
  const { versions: mapVersions, error: versionsError } = useMapVersions();
  const [version, setVersion] = useState(map.currentVersion);
  const [objects, setObjects] = useState<GeoObject[]>([]);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [objectsError, setObjectsError] = useState<string | null>(null);

  // Ensure map.currentVersion is always selectable, even if it isn't
  // (yet) present in the fetched versions list, and even if that fetch
  // failed entirely.
  const versions = useMemo(() => {
    const values = mapVersions.map((v) => v.version);
    if (!values.includes(map.currentVersion)) values.unshift(map.currentVersion);
    return values;
  }, [mapVersions, map.currentVersion]);

  useEffect(() => {
    setVersion(map.currentVersion);
    // Only re-run when a different map is opened.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [map]);

  const path = useCallback(
    (suffix: string) =>
      `/maps/${map.uuid}/version/${encodeURIComponent(version)}/geo-objects${suffix}`,
    [map, version],
  );

  const loadObjects = useCallback(async () => {
    if (!version) return;
    setObjectsError(null);
    try {
      setObjects(await apiJson<GeoObject[]>(path('')));
      setSelected(new Set());
    } catch (err) {
      setObjectsError(err instanceof Error ? err.message : String(err));
    }
  }, [version, path]);

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
    setObjectsError(null);
    try {
      await apiFetch(path('/' + id), { method: 'DELETE' });
      await loadObjects();
    } catch (err) {
      setObjectsError(err instanceof Error ? err.message : String(err));
    }
  };

  const deleteSelected = async () => {
    const ids = Array.from(selected);
    if (ids.length === 0) return;
    if (!confirm(`Delete ${ids.length} selected geo object(s)?`)) return;
    setObjectsError(null);
    try {
      for (const id of ids) {
        await apiFetch(path('/' + id), { method: 'DELETE' });
      }
    } catch (err) {
      setObjectsError(err instanceof Error ? err.message : String(err));
    } finally {
      await loadObjects();
    }
  };

  // Not memoized: several of these closures (saveObject, deleteObject, ...)
  // capture `version`/`path` and must stay fresh on every render rather
  // than risk going stale behind an incomplete dependency list.
  const value: GeoObjectsData = {
    versions,
    version,
    setVersion,
    objects,
    selected,
    error: objectsError ?? versionsError,
    toggleSelect,
    toggleSelectAll,
    saveObject,
    createObject,
    deleteObject,
    deleteSelected,
  };

  return <GeoObjectsContext.Provider value={value}>{children}</GeoObjectsContext.Provider>;
}

export function useGeoObjects(): GeoObjectsData {
  const ctx = useContext(GeoObjectsContext);
  if (!ctx) throw new Error('useGeoObjects must be used within GeoObjectsProvider');
  return ctx;
}
