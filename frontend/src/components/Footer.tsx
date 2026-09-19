import { useEffect, useState } from 'react';
import { Box, Link, Typography } from '@mui/material';
import { useApi } from '../api/ApiContext';
import { fetchBuildInfo } from '../lib/version';

export default function Footer() {
  const api = useApi();
  const [buildInfo, setBuildInfo] = useState('');

  useEffect(() => {
    fetchBuildInfo(api)
      .then(setBuildInfo)
      .catch(() => {});
  }, [api]);

  return (
    <Box component="footer" sx={{ textAlign: 'center', py: 3 }}>
      <Typography variant="caption" color="text.secondary">
        &copy; 2026 Nils Witt &middot; Tileserve &middot;{' '}
        <Link
          href="https://github.com/Nils-witt/Tileserve-GO"
          target="_blank"
          rel="noopener noreferrer"
          color="inherit"
        >
          GitHub
        </Link>
        {buildInfo}
      </Typography>
    </Box>
  );
}
