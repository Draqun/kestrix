import { Box, Paper } from '@mui/material';

/** AuthCard centers a fixed-width card on the viewport - the shared layout
 * for the pre-authentication pages (Login, Setup). */
export default function AuthCard({ children }) {
  return (
    <Box sx={{
      minHeight: '100vh', display: 'flex', alignItems: 'center', justifyContent: 'center',
      bgcolor: 'background.default', px: 2,
    }}>
      <Paper elevation={3} sx={{ p: 4, width: '100%', maxWidth: 380 }}>
        {children}
      </Paper>
    </Box>
  );
}
