import { createTheme } from '@mui/material/styles';
import { amber, blueGrey, teal, grey } from '@mui/material/colors';

const common = {
  typography: {
    fontFamily: '"Roboto Mono", "Roboto", "Helvetica", "Arial", sans-serif',
    h4: { fontWeight: 600 },
    h6: { fontWeight: 500 },
  },
  shape: { borderRadius: 12 },
  components: {
    MuiCard: {
      styleOverrides: {
        root: { backgroundImage: 'none' },
      },
    },
  },
};

export const lightTheme = createTheme({
  ...common,
  palette: {
    mode: 'light',
    primary: { main: teal[700] },
    secondary: { main: blueGrey[500] },
    background: {
      default: '#f5f7fa',
      paper: '#ffffff',
    },
  },
});

export const darkTheme = createTheme({
  ...common,
  palette: {
    mode: 'dark',
    primary: { main: amber[400] },
    secondary: { main: grey[400] },
    background: {
      default: '#121212',
      paper: '#1e1e2e',
    },
  },
});
