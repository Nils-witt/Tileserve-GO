import { Alert } from '@mui/material';

export default function ErrorBanner({ message }: { message: string | null }) {
  if (!message) return null;
  return (
    <Alert severity="error" sx={{ mt: 1.5, mb: 1.5 }}>
      {message}
    </Alert>
  );
}
