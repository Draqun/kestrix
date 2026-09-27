import { useState } from 'react';
import {
  Alert,
  Box,
  Button,
  Card,
  CardContent,
  Chip,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  LinearProgress,
  List,
  ListItem,
  ListItemText,
  Stack,
  TextField,
  Typography,
} from '@mui/material';
import { Add as AddIcon, Dns as RunnersIcon } from '@mui/icons-material';
import { createRunner, revokeRunner } from '../../api/client.js';
import { useAuth } from '../../context/AuthContext.jsx';
import useRunners from '../../hooks/useRunners.js';
import CopyButton from '../CopyButton.jsx';

const STATUS_COLOR = { busy: 'info', online: 'success', offline: 'warning', 'never seen': 'default' };
const EMPTY_FORM = { name: '', tags: '' };

function parseTags(text) {
  return text.split(',').map((t) => t.trim()).filter(Boolean);
}

function lastSeenLabel(runner) {
  return runner.last_seen_at ? `last seen ${new Date(runner.last_seen_at).toLocaleString()}` : 'never polled';
}

export default function RunnersSection() {
  const { roleAtLeast } = useAuth();
  const isAdmin = roleAtLeast('admin');
  const { items, loading, error, setError, refresh } = useRunners(isAdmin);
  const [form, setForm] = useState(EMPTY_FORM);
  const [creating, setCreating] = useState(false);
  // registered holds the one response that carries the token; closing the
  // dialog drops it, so the token cannot be shown again.
  const [registered, setRegistered] = useState(null);
  // revoking is the id of the one revoke in flight; every Revoke button is
  // disabled meanwhile, so two requests never overlap and clear each other.
  const [revoking, setRevoking] = useState(null);

  if (!isAdmin) return null;

  // runAction is AccountsSection's mutation shape: clear the previous error,
  // run the call, refresh the list on success, show the message on failure.
  const runAction = async (action, onBusyChange) => {
    onBusyChange(true);
    setError(null);
    try {
      await action();
      await refresh();
    } catch (cause) {
      setError(cause.message);
    } finally {
      onBusyChange(false);
    }
  };

  const submitCreate = (event) => {
    event.preventDefault();
    return runAction(async () => {
      const created = await createRunner({ name: form.name, tags: parseTags(form.tags) });
      setForm(EMPTY_FORM);
      setRegistered(created);
    }, setCreating);
  };

  const revoke = (runner) => {
    if (!window.confirm(`Revoke runner "${runner.name}"? Its token stops working immediately.`)) return undefined;
    return runAction(() => revokeRunner(runner.id), (busy) => setRevoking(busy ? runner.id : null));
  };

  const command = registered
    ? `kestrix runner --server ${window.location.origin} --token ${registered.token}`
    : '';

  return (
    <Card sx={{ mt: 3 }}>
      <CardContent>
        <Stack direction="row" spacing={1} sx={{ alignItems: 'center', mb: 2 }}>
          <RunnersIcon />
          <Box>
            <Typography component="h2" variant="h5">Runners</Typography>
            <Typography variant="body2" color="text.secondary">
              Register machines that execute remote-mode tasks. A task with runner tags only runs on a runner that has all of them.
            </Typography>
          </Box>
        </Stack>

        {loading && <LinearProgress sx={{ mb: 2 }} />}
        {error && <Alert severity="error" sx={{ mb: 2 }}>{error}</Alert>}

        <Box component="form" onSubmit={submitCreate} sx={{ mb: 2 }}>
          <Stack direction={{ xs: 'column', sm: 'row' }} spacing={1} sx={{ alignItems: { sm: 'center' } }}>
            <TextField
              label="Runner name"
              size="small"
              required
              disabled={creating}
              value={form.name}
              onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))}
            />
            <TextField
              label="Tags (comma-separated)"
              size="small"
              disabled={creating}
              value={form.tags}
              onChange={(e) => setForm((f) => ({ ...f, tags: e.target.value }))}
            />
            <Button type="submit" variant="contained" startIcon={<AddIcon />} disabled={creating} sx={{ whiteSpace: 'nowrap', flexShrink: 0 }}>Register runner</Button>
          </Stack>
        </Box>

        {!loading && items.length === 0 && <Alert severity="info">No runners registered.</Alert>}

        <List disablePadding>
          {items.map((runner) => (
            <ListItem key={runner.id} divider disableGutters data-testid={`runner-row-${runner.id}`}>
              <Stack direction={{ xs: 'column', sm: 'row' }} spacing={1} sx={{ alignItems: { sm: 'center' }, width: '100%' }}>
                <ListItemText sx={{ minWidth: 0, my: 0, flexGrow: 1 }} primary={runner.name} secondary={lastSeenLabel(runner)} />
                {/* one wrapping row, so chips keep their size instead of stretching on a narrow screen */}
                <Stack direction="row" spacing={1} useFlexGap sx={{ flexWrap: 'wrap', alignItems: 'center' }}>
                  {(runner.tags || []).map((tag) => <Chip key={tag} label={tag} size="small" variant="outlined" />)}
                  <Chip label={runner.status} size="small" color={STATUS_COLOR[runner.status] || 'default'} />
                  <Button size="small" color="error" disabled={revoking !== null} onClick={() => revoke(runner)}>Revoke</Button>
                </Stack>
              </Stack>
            </ListItem>
          ))}
        </List>
      </CardContent>

      <Dialog open={Boolean(registered)} onClose={() => setRegistered(null)} maxWidth="sm" fullWidth>
        <DialogTitle>Runner registered</DialogTitle>
        <DialogContent>
          <Alert severity="warning" sx={{ mb: 2 }}>
            This token is shown once. Store it on the runner machine now - it cannot be displayed again.
          </Alert>
          <Typography variant="body2" sx={{ mb: 1 }}>Start the runner with:</Typography>
          <Box sx={{ position: 'relative' }}>
            <Box
              component="pre"
              data-testid="runner-command"
              sx={{ m: 0, p: 1.5, pr: 5, bgcolor: 'action.hover', borderRadius: 1, whiteSpace: 'pre-wrap', wordBreak: 'break-all', fontSize: '0.85rem' }}
            >
              {command}
            </Box>
            <CopyButton text={command} />
          </Box>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setRegistered(null)}>Done</Button>
        </DialogActions>
      </Dialog>
    </Card>
  );
}
