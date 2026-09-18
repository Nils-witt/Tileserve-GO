import { lazy, type ReactNode, Suspense, useMemo } from 'react';
import { Navigate, Outlet, Route, Routes } from 'react-router-dom';
import { Box, CircularProgress, CssBaseline, ThemeProvider, useMediaQuery } from '@mui/material';
import { AuthProvider, useAuth } from './auth/AuthContext';
import ProtectedRoute from './auth/ProtectedRoute';
import LoginPage from './pages/LoginPage';
import { createAppTheme } from './theme';
import { AdminDataProvider, useAdminData } from './features/AdminDataContext.tsx';
import BaseLayout from './components/BaseLayout.tsx';

// Route-level components are code-split so a user only downloads the tabs
// they actually visit (admin-only tabs, the geo-objects editor) instead of
// bundling everything into the main chunk.
const MapsListPage = lazy(() => import('./pages/MapsListPage.tsx'));
const GeoObjectsPage = lazy(() => import('./features/maps/GeoObjectsPage.tsx'));
const UsersTab = lazy(() => import('./features/users/UsersTab.tsx'));
const GroupsTab = lazy(() => import('./features/groups/GroupsTab.tsx'));
const SyncTab = lazy(() => import('./features/sync/SyncTab.tsx'));
const AuditTab = lazy(() => import('./features/audit/AuditTab.tsx'));

function RouteFallback() {
  return (
    <Box sx={{ display: 'flex', justifyContent: 'center', py: 6 }}>
      <CircularProgress />
    </Box>
  );
}

function IndexRedirect() {
  const { isAuthenticated } = useAuth();
  return <Navigate to={isAuthenticated ? '/ui' : '/ui/login'} replace />;
}

/** Guards the admin-only routes against being opened directly by URL —
 * their tab is already hidden from non-admins, but the route itself must
 * also refuse them. */
function AdminOnlyRoute({ children }: { children: ReactNode }) {
  const { isAdmin } = useAdminData();
  return isAdmin ? <>{children}</> : <> Not authorized</>;
}

export default function App() {
  const prefersDarkMode = useMediaQuery('(prefers-color-scheme: dark)');
  const theme = useMemo(
    () => createAppTheme(prefersDarkMode ? 'dark' : 'light'),
    [prefersDarkMode],
  );

  return (
    <ThemeProvider theme={theme}>
      <CssBaseline />
      <AuthProvider>
        <AdminDataProvider>
          <Suspense fallback={<RouteFallback />}>
            <Routes>
              <Route index element={<IndexRedirect />} />
              <Route path="/ui/login" element={<LoginPage />} />
              <Route element={<BaseLayout />}>
                <Route path="ui" element={<ProtectedRoute />}>
                  <Route index element={<Navigate to={'/ui/maps'} replace />} />
                  <Route path="maps">
                    <Route index element={<MapsListPage />} />
                    <Route path=":uuid/geo-objects" element={<GeoObjectsPage />} />
                  </Route>
                  <Route
                    element={
                      <AdminOnlyRoute>
                        <Outlet />
                      </AdminOnlyRoute>
                    }
                  >
                    <Route path="users" element={<UsersTab />} />
                    <Route path="groups" element={<GroupsTab />} />
                    <Route path="sync" element={<SyncTab />} />
                    <Route path="audit" element={<AuditTab />} />
                  </Route>
                </Route>
              </Route>
              <Route path="*" element={<IndexRedirect />} />
            </Routes>
          </Suspense>
        </AdminDataProvider>
      </AuthProvider>
    </ThemeProvider>
  );
}
