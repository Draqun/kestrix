import { useEffect, useState } from 'react';
import {
  Alert,
  Button,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  FormControl,
  InputLabel,
  MenuItem,
  Select,
  Stack,
  TextField,
  Typography,
} from '@mui/material';
import { createCredential, rotateCredential } from '../../api/client.js';
import {
  CREDENTIAL_TYPES,
  changeCredentialType,
  credentialFormDefaults,
  credentialPayload,
} from '../../lib/credentialForm.js';

export default function CredentialFormDialog({ open, credential, onClose, onSuccess }) {
  const [form, setForm] = useState(() => credentialFormDefaults(credential));
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState(null);
  const editing = Boolean(credential?.ref);

  useEffect(() => {
    if (open) {
      setForm(credentialFormDefaults(credential));
      setError(null);
    }
  }, [open, credential]);

  const update = (field) => (event) => {
    setForm(current => ({ ...current, [field]: event.target.value }));
  };

  const close = () => {
    if (busy) return;
    setForm(credentialFormDefaults());
    setError(null);
    onClose?.();
  };

  const submit = async (event) => {
    event.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const payload = credentialPayload(form);
      const metadata = editing
        ? await rotateCredential(credential.ref, payload)
        : await createCredential(payload);
      await onSuccess?.(metadata);
      setForm(credentialFormDefaults());
      onClose?.();
    } catch (cause) {
      setError(cause.message || 'Credential operation failed');
    } finally {
      setBusy(false);
    }
  };

  const field = (key, label, props = {}) => (
    <TextField
      label={label}
      value={form[key]}
      onChange={update(key)}
      size="small"
      fullWidth
      {...props}
    />
  );

  return (
    <Dialog open={Boolean(open)} onClose={close} maxWidth="sm" fullWidth>
      <DialogTitle>{editing ? `Rotate ${credential.name}` : 'New credential'}</DialogTitle>
      <DialogContent>
        <form id="credential-form" onSubmit={submit}>
          <Stack spacing={2} sx={{ mt: 1 }}>
            <TypographyHint />
            {error && <Alert severity="error">{error}</Alert>}
            {field('name', 'Display name', { required: true, autoFocus: true })}
            <FormControl size="small" fullWidth>
              <InputLabel id="credential-type-label">Type</InputLabel>
              <Select
                labelId="credential-type-label"
                label="Type"
                value={form.type}
                onChange={(event) => setForm(current => changeCredentialType(current, event.target.value))}
              >
                {CREDENTIAL_TYPES.map(type => (
                  <MenuItem key={type.value} value={type.value}>{type.label}</MenuItem>
                ))}
              </Select>
            </FormControl>
            {field('provider', 'Provider (optional)', { placeholder: 'github, gitlab, bitbucket' })}
            {field('host', 'Host (optional)', { placeholder: 'git.example.com' })}
            {form.type === 'token' && field('token', editing ? 'Replacement token' : 'Token', { type: 'password', required: true })}
            {form.type === 'basic' && (
              <>
                {field('username', 'Username', { required: true })}
                {field('password', editing ? 'Replacement password' : 'Password', { type: 'password', required: true })}
              </>
            )}
            {form.type === 'ssh' && (
              <>
                {field('private_key', editing ? 'Replacement private key' : 'Private key', { multiline: true, minRows: 3, required: true })}
                {field('known_hosts', 'Known hosts (optional)', { multiline: true, minRows: 2 })}
              </>
            )}
          </Stack>
        </form>
      </DialogContent>
      <DialogActions>
        <Button onClick={close} disabled={busy}>Cancel</Button>
        <Button type="submit" form="credential-form" variant="contained" disabled={busy || !form.name.trim()}>
          {busy ? 'Saving…' : editing ? 'Rotate credential' : 'Create credential'}
        </Button>
      </DialogActions>
    </Dialog>
  );
}

function TypographyHint() {
  return (
    <Typography variant="body2" color="text.secondary">
      Secrets are write-only. Kestrix stores them encrypted and returns metadata only.
    </Typography>
  );
}
