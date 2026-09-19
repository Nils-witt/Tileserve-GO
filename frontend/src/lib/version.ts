import type { ApiClient } from '../api/ApiClient';

/** useBuildInfo-free helper: fetches GET /version for the footer build-info
 * string, matching both previous pages' unauthenticated footer fetch. */
export async function fetchBuildInfo(api: ApiClient): Promise<string> {
  const info = await api.getVersion();
  return ' · ' + (info.version ? info.version + ' · ' : '') + info.commit;
}
