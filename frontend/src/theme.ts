import { createTheme, type Theme } from '@mui/material/styles';

export function createAppTheme(mode: 'light' | 'dark'): Theme {
  return createTheme({
    palette: { mode },
    shape: { borderRadius: 8 },
    components: {
      MuiTableCell: {
        styleOverrides: {
          root: { verticalAlign: 'top' },
        },
      },
    },
  });
}
