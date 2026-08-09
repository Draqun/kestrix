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
import CredentialFormDialog from './CredentialFormDialog.jsx';

export default function CredentialSelectField({
  value,
  onChange,
  credentials,
  onCredentialsChanged,
  helperText = 'Choose public access to clear the saved credential.',
}) {
  const [createOpen, setCreateOpen] = useState(false);
  const active = activeCredentialMetadata(credentials);
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
      />
    </>
  );
}
