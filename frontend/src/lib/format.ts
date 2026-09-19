export function formatBytes(n: number): string {
  if (!Number.isFinite(n)) return '';
  const units = ['B', 'KB', 'MB', 'GB'];
  let i = 0;
  let value = n;
  while (value >= 1024 && i < units.length - 1) {
    value /= 1024;
    i++;
  }
  return value.toFixed(i === 0 ? 0 : 1) + ' ' + units[i];
}

export function fmtDate(iso: string): string {
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleString();
}
