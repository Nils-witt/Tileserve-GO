import { useCallback, useRef } from 'react';
import * as maplibregl from 'maplibre-gl';
import 'maplibre-gl/dist/maplibre-gl.css';
import { Box } from '@mui/material';
import { apiJson, getToken } from '../../api/client';
import type { MapBounds, MapSummary } from '../../api/types';
import Modal from '../../components/Modal';

export default function PreviewModal({
  map,
  onClose,
}: {
  map: MapSummary | null;
  onClose: () => void;
}) {
  // const containerRef = useRef<HTMLDivElement | null>(null);
  const mapRef = useRef<maplibregl.Map | null>(null);
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
        const tileUrl =
          window.location.origin +
          '/maps/' +
          map.uuid +
          '/version/' +
          map.currentVersion +
          '/{z}/{x}/{y}.png?token=' +
          encodeURIComponent(getToken() ?? '');
        console.log('Preview tile URL:', tileUrl);
        if (node?.children.length > 0) return;

        (async () => {
          let center: [number, number] = [0, 0];
          let zoom = 1;
          try {
            const bounds = await apiJson<MapBounds>(
              '/maps/' + map.uuid + '/version/' + map.currentVersion + '/bounds',
            );
            center = [bounds.centerLng, bounds.centerLat];
            zoom = bounds.minZoom;
            console.log(bounds);
          } catch {
            // fall back to the world view above
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
        })();
      }
    },
    [map],
  );

  return (
    <Modal
      open={!!map}
      title={map ? `${map.name} — v${map.currentVersion}` : 'Preview'}
      onClose={onClose}
      variant="full"
      noPadding
    >
      <Box ref={containerRef} sx={{ flex: 1, minHeight: 0 }}></Box>
    </Modal>
  );
}
