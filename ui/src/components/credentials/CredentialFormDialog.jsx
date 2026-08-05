import { useEffect, useState } from 'react';
import {
  Alert,
  Button,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  MenuItem,
  Stack,
  Typography,
} from '@mui/material';
import { createCredential, rotateCredential } from '../../api/client.js';
import {
  CREDENTIAL_TYPES,
  changeCredentialType,
  credentialFieldDescriptors,
  credentialFormDefaults,
  credentialPayload,
  redactCredentialError,
} from '../../lib/credentialForm.js';
import CredentialField from './CredentialField.jsx';

export default function CredentialFormDialog({ open, credential, onClose, onSuccess }) {
  const [form, setForm] = useState(() => credentialFormDefaults(credential));
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState(null);
  const editing = Boolean(credential?.ref);
  const descriptors = credentialFieldDescriptors(form.type, { editing });

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
      setError(redactCredentialError(cause.message, form));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog
      open={Boolean(open)}
      onClose={close}
      maxWidth="sm"
      fullWidth
      slotProps={{
        paper: {
          sx: {
            m: { xs: 2, sm: 4 },
            maxHeight: { xs: 'calc(100% - 32px)', sm: 'calc(100% - 64px)' },
          },
        },
      }}
    >
      <DialogTitle>{editing ? `Rotate ${credential.name}` : 'New credential'}</DialogTitle>
      <DialogContent sx={{ px: { xs: 2, sm: 3 } }}>
        <form id="credential-form" onSubmit={submit}>
          <Stack spacing={2} sx={{ mt: 1 }}>
            <CredentialScopeNotice />
            {error && <Alert severity="error">{error}</Alert>}
            {descriptors.map(descriptor => (
              <CredentialField
                key={descriptor.key}
                descriptor={descriptor}
                value={form[descriptor.key]}
                onChange={descriptor.key === 'type'
                  ? (event) => setForm(current => changeCredentialType(current, event.target.value))
                  : update(descriptor.key)}
                autoFocus={descriptor.key === 'name'}
              >
                {descriptor.key === 'type' && CREDENTIAL_TYPES.map(type => (
                  <MenuItem key={type.value} value={type.value}>{type.label}</MenuItem>
                ))}
              </CredentialField>
            ))}
          </Stack>
        </form>
      </DialogContent>
      <DialogActions sx={{ flexWrap: 'wrap', gap: 0.5, px: { xs: 2, sm: 3 } }}>
        <Button onClick={close} disabled={busy}>Cancel</Button>
        <Button type="submit" form="credential-form" variant="contained" disabled={busy || !form.name.trim()}>
          {busy ? 'Saving…' : editing ? 'Rotate credential' : 'Create credential'}
        </Button>
      </DialogActions>
    </Dialog>
  );
}

function CredentialScopeNotice() {
  return (
    <Alert severity="info" sx={{ '& .MuiAlert-message': { minWidth: 0 } }}>
      <Stack spacing={0.75}>
        <Typography variant="body2">
          Credential secrets are encrypted and write-only. Kestrix returns metadata only.
        </Typography>
        <Typography variant="body2">
          Repository credentials authenticate Git source acquisition (clone, fetch, and ref
          resolution) only. They are not exposed to tasks or deployments; configure deployment
          secrets separately.
        </Typography>
      </Stack>
    </Alert>
  );
}
