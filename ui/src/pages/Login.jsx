import { useState } from 'react';
import { Box, Typography, TextField, Button, Alert, Stack } from '@mui/material';
import { useAuth } from '../context/AuthContext';
import AuthCard from '../components/AuthCard';

export default function Login() {
  const { login } = useAuth();
  const [name, setName] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState('');
  const [submitting, setSubmitting] = useState(false);

  const handleSubmit = async (e) => {
    e.preventDefault();
    setError('');
    setSubmitting(true);
    try {
      await login(name, password);
    } catch (err) {
      if (err.code === 'SESSION_NOT_STARTED') {
        // AuthContext signals this when login answered 200 but the session
        // cookie never came back - the classic symptom of a Secure cookie
        // on a plain-HTTP deployment; see design.md D4 risk.
        setError(
          'Signed in, but the session did not start. If this deployment is plain HTTP, ' +
          'check KESTRIX_COOKIE_SECURE (see docs/authentication.md).',
        );
      } else {
        setError(err.message || 'Invalid name or password');
      }
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <AuthCard>
      <Typography component="h1" variant="h5" sx={{ mb: 3, textAlign: 'center' }}>
        Sign in to Kestrix
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
            autoComplete="current-password"
          />
          <Button
            type="submit"
            variant="contained"
            fullWidth
            disabled={submitting || !name || !password}
          >
            {submitting ? 'Signing in…' : 'Sign in'}
          </Button>
        </Stack>
      </Box>
    </AuthCard>
  );
}
