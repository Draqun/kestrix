import { Box, Typography } from '@mui/material';
import logo from '../assets/kestrix.svg';

export default function Brand() {
  return (
    <Box sx={{ display: 'flex', alignItems: 'center', gap: 1.25 }}>
      <Box component="img" src={logo} alt="" sx={{ width: 32, height: 32 }} />
      <Typography component="span" sx={{ fontSize: 21, fontWeight: 600, letterSpacing: '-0.04em' }}>
        Kestrix
      </Typography>
    </Box>
  );
}
