import type { VersionInfo } from '../api/types';

/** useBuildInfo-free helper: fetches GET /version for the footer build-info
 * string, matching both previous pages' unauthenticated footer fetch. */
export async function fetchBuildInfo(): Promise<string> {
  const res = await fetch('/version');
  const info = (await res.json()) as VersionInfo;
  return ' · ' + (info.version ? info.version + ' · ' : '') + info.commit;
}
