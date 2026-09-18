import { Outlet, useNavigate } from 'react-router-dom';
import {
  AppBar,
  Box,
  Button,
  Container,
  IconButton,
  Menu,
  MenuItem,
  Toolbar,
  Tooltip,
  Typography,
} from '@mui/material';
import Footer from './Footer.tsx';
import { useAuth } from '../auth/AuthContext.tsx';
import { useAdminData } from '../features/AdminDataContext.tsx';
import { type MouseEvent, useState } from 'react';
import MenuIcon from '@mui/icons-material/Menu';

type ActionDef = {
  label: string;
  onClick: () => void;
};

export default function BaseLayout() {
  const navigate = useNavigate();
  const { username, logout } = useAuth();
  const { isAdmin } = useAdminData();
  const [anchorElNav, setAnchorElNav] = useState<null | HTMLElement>(null);
  const [anchorElUser, setAnchorElUser] = useState<null | HTMLElement>(null);

  const settingsActions: ActionDef[] = [{ label: 'Logout', onClick: () => logout() }];

  const tabs: any[] = [
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

  const navMenuItems: ActionDef[] = tabs.map((tab) => ({
    label: tab.label,
    onClick: () => {
      navigate(tab.to);
      handleCloseNavMenu();
    },
  }));

  const handleOpenNavMenu = (event: MouseEvent<HTMLElement>) => {
    setAnchorElNav(event.currentTarget);
  };
  const handleOpenUserMenu = (event: MouseEvent<HTMLElement>) => {
    setAnchorElUser(event.currentTarget);
  };

  const handleCloseNavMenu = () => {
    setAnchorElNav(null);
  };

  const handleCloseUserMenu = () => {
    setAnchorElUser(null);
  };

  return (
    <Box sx={{ minHeight: '100vh', bgcolor: 'background.default' }}>
      <AppBar position="static">
        <Container maxWidth="xl">
          <Toolbar disableGutters>
            <Typography
              variant="h6"
              noWrap
              component="a"
              href="#app-bar-with-responsive-menu"
              sx={{
                mr: 2,
                display: { xs: 'none', md: 'flex' },
                fontFamily: 'monospace',
                fontWeight: 700,
                letterSpacing: '.3rem',
                color: 'inherit',
                textDecoration: 'none',
              }}
            >
              LOGO
            </Typography>

            <Box sx={{ flexGrow: 1, display: { xs: 'flex', md: 'none' } }}>
              <IconButton
                size="large"
                aria-label="account of current user"
                aria-controls="menu-appbar"
                aria-haspopup="true"
                onClick={handleOpenNavMenu}
                color="inherit"
              >
                <MenuIcon />
              </IconButton>
              <Menu
                id="menu-appbar"
                anchorEl={anchorElNav}
                anchorOrigin={{
                  vertical: 'bottom',
                  horizontal: 'left',
                }}
                keepMounted
                transformOrigin={{
                  vertical: 'top',
                  horizontal: 'left',
                }}
                open={Boolean(anchorElNav)}
                onClose={handleCloseNavMenu}
                sx={{ display: { xs: 'block', md: 'none' } }}
              >
                {navMenuItems.map((item) => (
                  <MenuItem key={item.label} onClick={item.onClick}>
                    <Typography sx={{ textAlign: 'center' }}>{item.label}</Typography>
                  </MenuItem>
                ))}
              </Menu>
            </Box>
            <Typography
              variant="h5"
              noWrap
              component="a"
              href="#app-bar-with-responsive-menu"
              sx={{
                mr: 2,
                display: { xs: 'flex', md: 'none' },
                flexGrow: 1,
                fontFamily: 'monospace',
                fontWeight: 700,
                letterSpacing: '.3rem',
                color: 'inherit',
                textDecoration: 'none',
              }}
            >
              LOGO
            </Typography>
            <Box sx={{ flexGrow: 1, display: { xs: 'none', md: 'flex' } }}>
              {navMenuItems.map((item) => (
                <Button
                  key={item.label}
                  onClick={item.onClick}
                  sx={{ my: 2, color: 'white', display: 'block' }}
                >
                  {item.label}
                </Button>
              ))}
            </Box>
            <Box sx={{ flexGrow: 0 }}>
              <Tooltip title="Open settings">
                <Button onClick={handleOpenUserMenu} sx={{ p: 0 }}>
                  {username}
                </Button>
              </Tooltip>
              <Menu
                sx={{ mt: '45px' }}
                id="menu-appbar"
                anchorEl={anchorElUser}
                anchorOrigin={{
                  vertical: 'top',
                  horizontal: 'right',
                }}
                keepMounted
                transformOrigin={{
                  vertical: 'top',
                  horizontal: 'right',
                }}
                open={Boolean(anchorElUser)}
                onClose={handleCloseUserMenu}
              >
                {settingsActions.map((action) => (
                  <MenuItem key={action.label} onClick={action.onClick}>
                    <Typography sx={{ textAlign: 'center' }}>{action.label}</Typography>
                  </MenuItem>
                ))}
              </Menu>
            </Box>
          </Toolbar>
        </Container>
      </AppBar>

      <Container maxWidth="xl" sx={{ py: 3 }}>
        <Outlet />

        <Footer />
      </Container>
    </Box>
  );
}
