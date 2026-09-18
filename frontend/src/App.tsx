import { type ReactNode, useMemo } from 'react';
import { Navigate, Outlet, Route, Routes } from 'react-router-dom';
import { CssBaseline, ThemeProvider, useMediaQuery } from '@mui/material';
import { AuthProvider, useAuth } from './auth/AuthContext';
import ProtectedRoute from './auth/ProtectedRoute';
import LoginPage from './pages/LoginPage';
import { createAppTheme } from './theme';
import { AdminDataProvider, useAdminData } from './features/AdminDataContext.tsx';
import MapsListPage from './pages/MapsListPage.tsx';
import BaseLayout from './components/BaseLayout.tsx';
import GeoObjectsPage from './features/maps/GeoObjectsPage.tsx';
import UsersTab from './features/users/UsersTab.tsx';
import GroupsTab from './features/groups/GroupsTab.tsx';
import SyncTab from './features/sync/SyncTab.tsx';
import AuditTab from './features/audit/AuditTab.tsx';
import {setSessionFromHash} from "./api/client.ts";

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
  if (document.location.hash.length > 10) {
    setSessionFromHash(document.location.hash);
    window.location.href = '/ui';
    return <></>
  }

  return (
    <ThemeProvider theme={theme}>
      <CssBaseline />
      <AuthProvider>
        <AdminDataProvider>
          <Routes>
            <Route path="/ui/login" element={<LoginPage />} />
            <Route element={<BaseLayout />}>
              <Route path="/ui" element={<ProtectedRoute />}>
                <Route index element={<Navigate to="maps" replace />} />
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
            <Route path="/" element={<IndexRedirect />} />
          </Routes>
        </AdminDataProvider>
      </AuthProvider>
    </ThemeProvider>
  );
}
