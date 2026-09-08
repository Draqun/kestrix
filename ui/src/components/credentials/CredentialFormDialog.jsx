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

// MIN_REDACTED_SECRET_LENGTH mirrors the service layer's redaction floor
// (see runtime-secrets design.md D5): a value shorter than this is still
// injected into a task but is not guaranteed to be redacted from output.
const MIN_REDACTED_SECRET_LENGTH = 8;

export default function CredentialFormDialog({ open, credential, onClose, onSuccess, allowedTypes }) {
  // allowedTypes restricts which types this dialog instance offers - e.g. the
  // project credential selector's inline "Add credential" only wants Git
  // types, since a `secret` credential is never usable as a
  // repo_credential_ref (design D4). Omit the prop to offer every type.
  const availableTypes = allowedTypes
    ? CREDENTIAL_TYPES.filter(type => allowedTypes.includes(type.value))
    : CREDENTIAL_TYPES;
  const defaultsFor = (cred) => {
    const defaults = credentialFormDefaults(cred);
    if (allowedTypes && !allowedTypes.includes(defaults.type)) {
      return changeCredentialType(defaults, availableTypes[0]?.value);
    }
    return defaults;
  };

  const [form, setForm] = useState(() => defaultsFor(credential));
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState(null);
  const editing = Boolean(credential?.ref);
  const descriptors = credentialFieldDescriptors(form.type, { editing });
  const showShortSecretWarning = form.type === 'secret' && form.value.length > 0 && form.value.length < MIN_REDACTED_SECRET_LENGTH;

  useEffect(() => {
    if (open) {
      setForm(defaultsFor(credential));
      setError(null);
    }
  }, [open, credential]);

  const update = (field) => (event) => {
    setForm(current => ({ ...current, [field]: event.target.value }));
  };

  const close = () => {
    if (busy) return;
    setForm(defaultsFor());
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
      setForm(defaultsFor());
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
            {showShortSecretWarning && (
              <Alert severity="warning">
                Values under {MIN_REDACTED_SECRET_LENGTH} characters are not guaranteed to be
                redacted from run output.
              </Alert>
            )}
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
                {descriptor.key === 'type' && availableTypes.map(type => (
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
