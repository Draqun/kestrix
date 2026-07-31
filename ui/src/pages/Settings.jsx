import { Box, Typography } from '@mui/material';
import RepositoryCredentialsSection from '../components/credentials/RepositoryCredentialsSection.jsx';

export default function Settings() {
  return (
    <Box sx={{ width: '100%', maxWidth: 960, mx: 'auto' }}>
      <Typography component="h1" variant="h4" sx={{ mb: 1 }}>Settings</Typography>
      <Typography variant="body1" color="text.secondary" sx={{ mb: 3 }}>
        Manage application-wide integrations and protected repository access.
      </Typography>
      <RepositoryCredentialsSection />
    </Box>
  );
}
