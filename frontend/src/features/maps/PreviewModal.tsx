import { useCallback, useEffect, useRef, useState } from 'react';
import type * as maplibregl from 'maplibre-gl';
import { Box, Button, Stack, Typography } from '@mui/material';
import { apiJson, getToken } from '../../api/client';
import type { MapBounds, MapSummary } from '../../api/types';
import Modal from '../../components/Modal';

export default function PreviewModal({
  map,
  version,
  onClose,
  pickable = false,
  initialLatitude,
  initialLongitude,
  onPick,
}: {
  map: MapSummary | null;
  /** Tileset version to preview; defaults to `map.currentVersion`. */
  version?: string;
  onClose: () => void;
  /** When true, clicking/dragging on the preview drops a marker and shows
   * a "Use this location" action instead of a plain read-only preview. */
  pickable?: boolean;
  initialLatitude?: number;
  initialLongitude?: number;
  onPick?: (latitude: number, longitude: number) => void;
}) {
  // const containerRef = useRef<HTMLDivElement | null>(null);
  const mapRef = useRef<maplibregl.Map | null>(null);
  const markerRef = useRef<maplibregl.Marker | null>(null);
  const [position, setPosition] = useState<{ lat: number; lng: number } | null>(
    pickable && initialLatitude != null && initialLongitude != null
      ? { lat: initialLatitude, lng: initialLongitude }
      : null,
  );

  useEffect(() => {
    if (map && pickable) {
      setPosition(
        initialLatitude != null && initialLongitude != null
          ? { lat: initialLatitude, lng: initialLongitude }
          : null,
      );
    }
    // Only re-sync when the modal opens, not on every keystroke in the form.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [map, pickable]);
  /*
  useEffect(() => {
    console.log("Container ref:", containerRef.current);
    if (!map || !map.currentVersion || !containerRef.current) return;

    let cancelled = false;

    const tileUrl =
      window.location.origin +
      '/maps/' +
      map.uuid +
      '/version/' +
      map.currentVersion +
      '/{z}/{x}/{y}.png?token=' +
      encodeURIComponent(getToken() ?? '');
    console.log('Preview tile URL:', tileUrl);

    // Centered on the world at zoom 1 as a fallback if bounds can't be
    // determined; otherwise centered on the tileset at a zoom level that
    // actually has tiles, so the preview shows something immediately.
    (async () => {
      let center: [number, number] = [0, 0];
      let zoom = 1;
      try {
        const bounds = await apiJson<MapBounds>(
          '/maps/' + map.uuid + '/version/' + map.currentVersion + '/bounds',
        );
        center = [bounds.centerLng, bounds.centerLat];
        zoom = bounds.minZoom;
        console.log(bounds)
      } catch {
        // fall back to the world view above
      }
      if (cancelled || !containerRef.current) return;

      const instance = new maplibregl.Map({
        container: containerRef.current,
        style: {
          version: 8,
          sources: {
            tiles: {
              type: 'raster',
              tiles: [tileUrl],
              tileSize: 256,
            },
          },
          layers: [{ id: 'tiles', type: 'raster', source: 'tiles' }],
        },
        center,
        zoom,
      });
      instance.addControl(new maplibregl.NavigationControl());
      mapRef.current = instance;
    })();

    return () => {
      cancelled = true;
      mapRef.current?.remove();
      mapRef.current = null;
    };
  }, [containerRef.current, map]);
*/

  const containerRef = useCallback(
    (node: HTMLElement | null) => {
      if (node && map) {
        const tilesetVersion = version ?? map.currentVersion;
        const tileUrl =
          window.location.origin +
          '/maps/' +
          map.uuid +
          '/version/' +
          encodeURIComponent(tilesetVersion) +
          '/{z}/{x}/{y}.png?token=' +
          encodeURIComponent(getToken() ?? '');
        if (node?.children.length > 0) return;

        (async () => {
          let center: [number, number] = [0, 0];
          let zoom = 1;

          // maplibre-gl is a large dependency (~500 kB) only needed once a
          // preview is actually opened, so it's loaded on demand alongside
          // the bounds fetch rather than bundled into the main chunk.
          const [maplibregl, bounds] = await Promise.all([
            import('maplibre-gl').then(async (mod) => {
              await import('maplibre-gl/dist/maplibre-gl.css');
              return mod;
            }),
            apiJson<MapBounds>(
              '/maps/' + map.uuid + '/version/' + encodeURIComponent(tilesetVersion) + '/bounds',
            ).catch(() => null),
          ]);
          if (bounds) {
            center = [bounds.centerLng, bounds.centerLat];
            zoom = bounds.minZoom;
          }
          if (pickable && initialLatitude != null && initialLongitude != null) {
            center = [initialLongitude, initialLatitude];
          }

          if (node?.children.length > 0) return;
          const instance = new maplibregl.Map({
            container: node,
            style: {
              version: 8,
              sources: {
                tiles: {
                  type: 'raster',
                  tiles: [tileUrl],
                  tileSize: 256,
                },
              },
              layers: [{ id: 'tiles', type: 'raster', source: 'tiles' }],
            },
            center,
            zoom,
          });
          instance.addControl(new maplibregl.NavigationControl());
          mapRef.current = instance;

          if (pickable) {
            const marker = new maplibregl.Marker({ draggable: true }).setLngLat(center);
            if (position) marker.addTo(instance);
            marker.on('dragend', () => {
              const { lat, lng } = marker.getLngLat();
              setPosition({ lat, lng });
            });
            markerRef.current = marker;

            instance.on('click', (e) => {
              marker.setLngLat(e.lngLat).addTo(instance);
              setPosition({ lat: e.lngLat.lat, lng: e.lngLat.lng });
            });
          }
        })();
      }
    },
    [map, version, pickable, initialLatitude, initialLongitude, position],
  );

  const handleConfirm = () => {
    if (position) onPick?.(position.lat, position.lng);
    onClose();
  };

  return (
    <Modal
      open={!!map}
      title={
        map
          ? pickable
            ? `Pick location — ${map.name}`
            : `${map.name} — v${map.currentVersion}`
          : 'Preview'
      }
      onClose={onClose}
      variant="full"
      noPadding
      headerExtra={
        pickable ? (
          <Stack direction="row" spacing={2} sx={{ alignItems: 'center' }}>
            <Typography variant="body2" color="text.secondary">
              {position
                ? `${position.lat.toFixed(6)}, ${position.lng.toFixed(6)}`
                : 'Click the map to place a marker'}
            </Typography>
            <Button size="small" variant="contained" disabled={!position} onClick={handleConfirm}>
              Use this location
            </Button>
          </Stack>
        ) : undefined
      }
    >
      <Box ref={containerRef} sx={{ flex: 1, minHeight: 0 }}></Box>
    </Modal>
  );
}
