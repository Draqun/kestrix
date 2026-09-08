import { useState } from 'react';
import {
  Button,
  FormControl,
  FormHelperText,
  InputLabel,
  MenuItem,
  Select,
  Stack,
} from '@mui/material';
import { Add as AddIcon, Settings as SettingsIcon } from '@mui/icons-material';
import { Link as RouterLink } from 'react-router-dom';
import { activeCredentialMetadata } from '../../lib/projectForm.js';
import { CREDENTIAL_TYPES } from '../../lib/credentialForm.js';
import CredentialFormDialog from './CredentialFormDialog.jsx';

// GIT_CREDENTIAL_TYPES excludes `secret` - a repo_credential_ref never
// accepts one (design D4) - so the inline "Add credential" dialog here
// cannot create and auto-select a type the backend would reject.
const GIT_CREDENTIAL_TYPES = CREDENTIAL_TYPES.filter(type => type.value !== 'secret').map(type => type.value);

export default function CredentialSelectField({
  value,
  onChange,
  credentials,
  onCredentialsChanged,
  helperText = 'Choose public access to clear the saved credential.',
}) {
  const [createOpen, setCreateOpen] = useState(false);
  // A `secret` credential is a runtime value for tasks, not a Git
  // credential - see runtime-secrets design.md D4. It is filtered out here
  // even though it is active, so this project selector never offers it.
  const active = activeCredentialMetadata(credentials).filter(item => item.type !== 'secret');
  const selectedRevoked = (credentials || []).find(item => item.ref === value && item.revoked);

  const created = async (metadata) => {
    await onCredentialsChanged?.();
    onChange(metadata.ref);
  };

  return (
    <>
      <Stack
        direction={{ xs: 'column', sm: 'row' }}
        spacing={1}
        sx={{ alignItems: { sm: 'flex-start' } }}
      >
        <FormControl size="small" fullWidth>
          <InputLabel id="project-credential-label">Credential (optional)</InputLabel>
          <Select
            labelId="project-credential-label"
            label="Credential (optional)"
            value={value || ''}
            onChange={(event) => onChange(event.target.value)}
          >
            <MenuItem value="">Public access</MenuItem>
            {selectedRevoked && (
              <MenuItem value={selectedRevoked.ref} disabled>
                {selectedRevoked.name} · revoked
              </MenuItem>
            )}
            {active.map(item => (
              <MenuItem key={item.ref} value={item.ref}>
                {item.name} · {item.provider || item.type}
              </MenuItem>
            ))}
          </Select>
          <FormHelperText>{helperText}</FormHelperText>
        </FormControl>
        <Stack direction="row" spacing={1} sx={{ flexShrink: 0 }}>
          <Button startIcon={<AddIcon />} onClick={() => setCreateOpen(true)}>Add credential</Button>
          <Button component={RouterLink} to="/settings" startIcon={<SettingsIcon />}>Manage</Button>
        </Stack>
      </Stack>

      <CredentialFormDialog
        open={createOpen}
        onClose={() => setCreateOpen(false)}
        onSuccess={created}
        allowedTypes={GIT_CREDENTIAL_TYPES}
      />
    </>
  );
}
