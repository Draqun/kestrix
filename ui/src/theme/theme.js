import { alpha, createTheme } from '@mui/material/styles';

function workspaceTheme(mode) {
  const dark = mode === 'dark';
  const ink = dark ? '#ededed' : '#171b1c';
  const accent = dark ? '#76c6d0' : '#167486';
  const border = dark ? '#2c2c2c' : '#e5e9e9';
  return createTheme({
    palette: {
      mode,
      primary: { main: accent },
      secondary: { main: dark ? '#b4b4b4' : '#5d6669' },
      background: { default: dark ? '#151515' : '#f2f6f6', paper: dark ? '#1b1b1b' : '#ffffff' },
      text: { primary: ink, secondary: dark ? '#ababab' : '#5d6669' },
      divider: border,
      success: { main: dark ? '#73c69b' : '#23764f' },
      error: { main: dark ? '#ed9299' : '#b93846' },
      warning: { main: dark ? '#e2bd70' : '#8b621b' },
      info: { main: accent },
      action: { hover: alpha(ink, 0.04), selected: alpha(ink, 0.07) },
    },
    typography: {
      fontFamily: '"IBM Plex Sans", system-ui, sans-serif',
      fontSize: 14,
      h4: { fontSize: '1.5rem', fontWeight: 600, letterSpacing: '-0.035em' },
      h5: { fontSize: '1.375rem', fontWeight: 600, letterSpacing: '-0.025em' },
      h6: { fontSize: '1rem', fontWeight: 600, letterSpacing: '-0.015em' },
      button: { textTransform: 'none', fontWeight: 500 },
    },
    shape: { borderRadius: 8 },
    components: {
      MuiCssBaseline: { styleOverrides: {
        body: { fontVariantNumeric: 'tabular-nums' },
        'code, pre, kbd': { fontFamily: '"IBM Plex Mono", monospace' },
        '::selection': { backgroundColor: alpha(accent, 0.25) },
        ':focus-visible': { outline: `2px solid ${accent}`, outlineOffset: 3 },
        '@media (prefers-reduced-motion: reduce)': {
          '*, *::before, *::after': { animation: 'none !important', transition: 'none !important', scrollBehavior: 'auto !important' },
        },
      } },
      MuiPaper: { styleOverrides: { root: { backgroundImage: 'none' } } },
      MuiCard: { defaultProps: { elevation: 0 }, styleOverrides: { root: { border: `1px solid ${border}`, backgroundImage: 'none' } } },
      MuiButton: { defaultProps: { disableElevation: true }, styleOverrides: {
        root: { minHeight: 36, borderRadius: 6, '&.MuiButton-contained.MuiButton-colorPrimary': { backgroundColor: ink, color: dark ? '#171b1c' : '#ffffff', '&:hover': { backgroundColor: dark ? '#d4d4d4' : '#343b3d' } } },
      } },
      MuiIconButton: { styleOverrides: { root: { borderRadius: 6 } } },
      MuiTableCell: { styleOverrides: {
        root: { borderColor: border, padding: '14px 16px', fontVariantNumeric: 'tabular-nums' },
        head: { color: dark ? '#ababab' : '#5d6669', fontWeight: 500, fontSize: 12, backgroundColor: 'transparent' },
      } },
      MuiChip: { styleOverrides: { root: { borderRadius: 5, fontWeight: 500, fontSize: 12 }, outlined: { borderColor: border } } },
      MuiTab: { styleOverrides: { root: { textTransform: 'none', minHeight: 44 } } },
      MuiTabs: { styleOverrides: { indicator: { height: 2 } } },
      MuiOutlinedInput: { styleOverrides: { root: { borderRadius: 6 }, notchedOutline: { borderColor: dark ? '#555555' : '#bfcaca' } } },
      MuiBottomNavigationAction: { styleOverrides: { root: { color: dark ? '#ababab' : '#5d6669', '&.Mui-selected': { color: ink } } } },
    },
  });
}

export const lightTheme = workspaceTheme('light');
export const darkTheme = workspaceTheme('dark');
