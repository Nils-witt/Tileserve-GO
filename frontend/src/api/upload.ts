// XHR-based upload with progress events, ported from the previous UI's
// uploadVersion(): fetch() can't report upload progress, so this uses
// XMLHttpRequest directly for POST /maps/{id}/upload.

import { clearSession, getToken } from "./client";

export interface UploadProgress {
  loaded: number;
  total: number;
}

export interface UploadResult {
  ok: boolean;
  status: number;
  unauthorized: boolean;
  errorText?: string;
}

export function uploadMapVersion(
  mapId: string,
  file: File,
  onProgress: (progress: UploadProgress) => void,
): Promise<UploadResult> {
  return new Promise((resolve) => {
    const xhr = new XMLHttpRequest();
    xhr.open("POST", "/maps/" + mapId + "/upload");
    xhr.setRequestHeader("Authorization", "Bearer " + (getToken() ?? ""));
    xhr.setRequestHeader("Content-Type", file.type || "application/octet-stream");

    xhr.upload.onprogress = (e) => {
      if (!e.lengthComputable) return;
      onProgress({ loaded: e.loaded, total: e.total });
    };

    xhr.onload = () => {
      if (xhr.status === 401) {
        clearSession();
        resolve({ ok: false, status: xhr.status, unauthorized: true });
        return;
      }
      if (xhr.status < 200 || xhr.status >= 300) {
        resolve({ ok: false, status: xhr.status, unauthorized: false, errorText: xhr.responseText });
        return;
      }
      resolve({ ok: true, status: xhr.status, unauthorized: false });
    };

    xhr.onerror = () => {
      resolve({ ok: false, status: 0, unauthorized: false, errorText: "upload failed" });
    };

    xhr.send(file);
  });
}

export function formatBytes(n: number): string {
  if (!Number.isFinite(n)) return "";
  const units = ["B", "KB", "MB", "GB"];
  let i = 0;
  let value = n;
  while (value >= 1024 && i < units.length - 1) {
    value /= 1024;
    i++;
  }
  return value.toFixed(i === 0 ? 0 : 1) + " " + units[i];
}
