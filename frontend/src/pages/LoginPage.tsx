import { useEffect, useMemo, useState, type SubmitEvent } from 'react';
import { useLocation, useNavigate } from 'react-router-dom';
import {
  Alert,
  Box,
  Button,
  Container,
  createTheme,
  Divider,
  Paper,
  Stack,
  TextField,
  ThemeProvider,
  Typography,
  useMediaQuery,
} from '@mui/material';
import LoginIcon from '@mui/icons-material/Login';
import { useAuth } from '../auth/AuthContext';
import { useAuthMethods } from '../auth/useAuthMethods';
import Footer from '../components/Footer';
import './LoginPage.scss';

export default function LoginPage() {
  const auth = useAuth();
  const authMethods = useAuthMethods();
  const routerLocation = useLocation();
  const from = (routerLocation.state as { from?: string } | null)?.from ?? null;

  const navigate = useNavigate();

  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const prefersDarkMode = useMediaQuery('(prefers-color-scheme: dark)');
  const theme = useMemo(
    () => createTheme({ palette: { mode: prefersDarkMode ? 'dark' : 'light' } }),
    [prefersDarkMode],
  );

  useEffect(() => {
    if (auth.sessionMessage) {
      setError(auth.sessionMessage);
      auth.clearSessionMessage();
    }
    // Only re-run when the session-expired message actually changes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [auth.sessionMessage]);

  const handleSubmit = async (e: SubmitEvent<HTMLFormElement>) => {
    e.preventDefault();
    setError(null);
    setSubmitting(true);
    try {
      await auth.login(username, password);
      await navigate(from ?? '/ui', { replace: true });
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Login failed');
    } finally {
      setSubmitting(false);
    }
  };

  const handleSSO = () => {
    window.location.href = '/login/oidc?redirect=' + encodeURIComponent(from ?? '/login');
  };

  return (
    <ThemeProvider theme={theme}>
      <Box className="login-page" sx={{ bgcolor: 'background.default', color: 'text.primary' }}>
        <Container maxWidth="xs" disableGutters>
          <Paper elevation={3}>
            <Typography variant="h5" component="h1" gutterBottom>
              Sign in to get a token
            </Typography>
            <Box component="form" onSubmit={handleSubmit} noValidate>
              <Stack spacing={2}>
                <TextField
                  id="username"
                  label="Username"
                  autoComplete="username"
                  required
                  fullWidth
                  value={username}
                  onChange={(e) => setUsername(e.target.value)}
                />
                <TextField
                  id="password"
                  label="Password"
                  type="password"
                  autoComplete="current-password"
                  required
                  fullWidth
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                />
                <Button
                  type="submit"
                  variant="contained"
                  fullWidth
                  disabled={submitting}
                  startIcon={<LoginIcon />}
                >
                  {submitting ? 'Signing in…' : 'Sign in'}
                </Button>
              </Stack>
            </Box>
            {authMethods.oidc && (
              <>
                <Divider>or</Divider>
                <Button type="button" variant="outlined" fullWidth onClick={handleSSO}>
                  Sign in with SSO
                </Button>
              </>
            )}
            {error && <Alert severity="error">{error}</Alert>}
          </Paper>
        </Container>
        <Footer />
      </Box>
    </ThemeProvider>
  );
}
