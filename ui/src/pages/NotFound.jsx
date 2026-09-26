import { Box, Button, Typography } from '@mui/material';
import { Link as RouterLink } from 'react-router-dom';

export default function NotFound() {
  return (
    <Box sx={{ textAlign: 'center', mt: 8 }}>
      <Typography variant="h4" gutterBottom>Page not found</Typography>
      <Typography color="text.secondary" sx={{ mb: 3 }}>
        There is nothing at this address.
      </Typography>
      <Button variant="contained" component={RouterLink} to="/">Go to dashboard</Button>
    </Box>
  );
}
