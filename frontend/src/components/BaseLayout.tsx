import { Outlet } from 'react-router-dom';
import { AppBar, Box, Button, Container, Toolbar, Typography } from '@mui/material';
import Tabs, { type TabDef } from './Tabs.tsx';
import Footer from './Footer.tsx';
import { useAuth } from '../auth/AuthContext.tsx';
import { useAdminData } from '../features/AdminDataContext.tsx';

export default function BaseLayout() {
  const { username, logout } = useAuth();
  const { isAdmin } = useAdminData();

  const tabs: TabDef[] = [
    { to: '/ui/maps', label: 'Maps' },
    ...(isAdmin
      ? [
          { to: '/ui/users', label: 'Users' },
          { to: '/ui/groups', label: 'Groups' },
          { to: '/ui/sync', label: 'Sync' },
          { to: '/ui/audit', label: 'Audit log' },
        ]
      : []),
  ];
  return (
    <Box sx={{ minHeight: '100vh', bgcolor: 'background.default' }}>
      <AppBar
        position="static"
        color="transparent"
        elevation={0}
        sx={{ borderBottom: 1, borderColor: 'divider' }}
      >
        <Toolbar sx={{ gap: 2 }}>
          <Typography variant="h6" component="h1" sx={{ flexGrow: 1 }}>
            tileserve-go
          </Typography>
          <Typography variant="body2" color="text.secondary">
            {username}
          </Typography>
          <Button variant="outlined" size="small" onClick={logout}>
            Log out
          </Button>
        </Toolbar>
      </AppBar>

      <Container maxWidth="xl" sx={{ py: 3 }}>
        <Tabs tabs={tabs} />
        <Outlet />

        <Footer />
      </Container>
    </Box>
  );
}
