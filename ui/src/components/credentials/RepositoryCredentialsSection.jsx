import { useState } from 'react';
import {
  Alert,
  Box,
  Button,
  Card,
  CardContent,
  Chip,
  LinearProgress,
  List,
  ListItem,
  ListItemText,
  Stack,
  Typography,
} from '@mui/material';
import { Add as AddIcon, VpnKey as CredentialIcon } from '@mui/icons-material';
import { revokeCredential } from '../../api/client.js';
import useCredentials from '../../hooks/useCredentials.js';
import CredentialFormDialog from './CredentialFormDialog.jsx';

export default function RepositoryCredentialsSection() {
  const { items, loading, error, setError, refresh } = useCredentials();
  const [dialogOpen, setDialogOpen] = useState(false);
  const [editing, setEditing] = useState(null);

  const openCreate = () => {
    setEditing(null);
    setDialogOpen(true);
  };

  const openRotate = (credential) => {
    setEditing(credential);
    setDialogOpen(true);
  };

  const revoke = async (credential) => {
    if (!window.confirm(`Revoke credential "${credential.name}"?`)) return;
    setError(null);
    try {
      await revokeCredential(credential.ref);
      await refresh();
    } catch (cause) {
      setError(cause.message);
    }
  };

  return (
    <Card>
      <CardContent>
        <Stack
          direction={{ xs: 'column', sm: 'row' }}
          spacing={2}
          sx={{ mb: 2, alignItems: { sm: 'center' } }}
        >
          <Box sx={{ display: 'flex', alignItems: 'center', flexGrow: 1 }}>
            <CredentialIcon sx={{ mr: 1 }} />
            <Box>
              <Typography component="h2" variant="h5">Repository credentials</Typography>
              <Typography variant="body2" color="text.secondary">
                Provider-neutral credentials for private Git repositories and runtime secrets. Secret values are never displayed.
              </Typography>
            </Box>
          </Box>
          <Button variant="contained" startIcon={<AddIcon />} onClick={openCreate} sx={{ whiteSpace: 'nowrap', flexShrink: 0 }}>Add credential</Button>
        </Stack>

        {loading && <LinearProgress sx={{ mb: 2 }} />}
        {error && <Alert severity="error" sx={{ mb: 2 }}>{error}</Alert>}
        {!loading && items.length === 0 && (
          <Alert severity="info">No repository credentials configured.</Alert>
        )}
        <List disablePadding>
          {items.map(item => (
            <ListItem
              key={item.ref}
              divider
              disableGutters
              sx={{
                alignItems: { xs: 'stretch', sm: 'center' },
                flexDirection: { xs: 'column', sm: 'row' },
                gap: 1,
                py: 1.5,
              }}
            >
              <ListItemText
                sx={{ minWidth: 0, my: 0 }}
                primary={(
                  <Stack direction="row" spacing={1} sx={{ alignItems: 'center' }}>
                    <span>{item.name}</span>
                    {item.revoked && <Chip label="Revoked" size="small" color="default" />}
                  </Stack>
                )}
                secondary={`${item.type} · ${item.provider || 'generic'} · ${item.host || 'any host'} · ${item.ref}`}
              />
              <Stack
                data-testid={`credential-actions-${item.ref}`}
                direction="row"
                spacing={1}
                sx={{ flexShrink: 0, justifyContent: { xs: 'flex-end', sm: 'initial' } }}
              >
                <Button size="small" onClick={() => openRotate(item)}>Rotate</Button>
                <Button size="small" color="error" disabled={item.revoked} onClick={() => revoke(item)}>Revoke</Button>
              </Stack>
            </ListItem>
          ))}
        </List>
      </CardContent>

      <CredentialFormDialog
        open={dialogOpen}
        credential={editing}
        onClose={() => setDialogOpen(false)}
        onSuccess={refresh}
      />
    </Card>
  );
}
