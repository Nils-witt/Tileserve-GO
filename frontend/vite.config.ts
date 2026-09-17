import react from '@vitejs/plugin-react';
import { defineConfig } from 'vite';

// Proxies every backend path to the local Go server during `npm run dev`,
// so the frontend gets live reload without rebuilding the Go binary.
const backendTarget = 'http://localhost:8085';
const proxiedPaths = [
  '/login',
  '/refresh',
  '/auth',
  '/maps',
  '/users',
  '/groups',
  '/sync',
  '/audit-logs',
  '/permissions',
  '/keys',
  '/server',
  '/version',
  '/healthz',
];

const targetConfig = { target: backendTarget, changeOrigin: true };

export default defineConfig({
  plugins: [react()],
  server: {
    proxy: Object.fromEntries(proxiedPaths.map((path) => [path, targetConfig])),
  },
});
