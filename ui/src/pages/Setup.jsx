import { useState } from 'react';
import { Box, Typography, TextField, Button, Alert, Stack } from '@mui/material';
import { useAuth } from '../context/AuthContext';
import AuthCard from '../components/AuthCard';

export default function Setup() {
  const { completeSetup } = useAuth();
  const [name, setName] = useState('');
  const [password, setPassword] = useState('');
  const [confirmPassword, setConfirmPassword] = useState('');
  const [error, setError] = useState('');
  const [submitting, setSubmitting] = useState(false);

  const handleSubmit = async (e) => {
    e.preventDefault();
    setError('');
    if (password !== confirmPassword) {
      setError('Passwords do not match');
      return;
    }
    setSubmitting(true);
    try {
      await completeSetup(name, password);
    } catch (err) {
      if (err.status === 409) {
        // Someone else finished setup in the meantime; AuthContext already
        // cleared setupRequired, so the ordinary login form takes over.
        setError('Setup was already completed. Please sign in instead.');
      } else {
        setError(err.message || 'Setup failed');
      }
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <AuthCard>
      <Typography component="h1" variant="h5" sx={{ mb: 1, textAlign: 'center' }}>
        Create the first administrator
      </Typography>
      <Typography variant="body2" color="text.secondary" sx={{ mb: 3, textAlign: 'center' }}>
        This account will have full admin access to this Kestrix instance.
      </Typography>
      <Box component="form" onSubmit={handleSubmit}>
        <Stack spacing={2}>
          {error && <Alert severity="error">{error}</Alert>}
          <TextField
            label="Name"
            value={name}
            onChange={(e) => setName(e.target.value)}
            autoFocus
            fullWidth
            autoComplete="username"
          />
          <TextField
            label="Password"
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            fullWidth
            autoComplete="new-password"
          />
          <TextField
            label="Confirm password"
            type="password"
            value={confirmPassword}
            onChange={(e) => setConfirmPassword(e.target.value)}
            fullWidth
            autoComplete="new-password"
          />
          <Button
            type="submit"
            variant="contained"
            fullWidth
            disabled={submitting || !name || !password || !confirmPassword}
          >
            {submitting ? 'Creating…' : 'Create administrator'}
          </Button>
        </Stack>
      </Box>
    </AuthCard>
  );
}
