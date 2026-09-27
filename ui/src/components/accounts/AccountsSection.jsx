import { useState } from 'react';
import {
  Alert,
  Box,
  Button,
  Card,
  CardContent,
  Chip,
  FormControl,
  IconButton,
  InputLabel,
  LinearProgress,
  List,
  ListItem,
  ListItemText,
  MenuItem,
  Select,
  Stack,
  TextField,
  Typography,
} from '@mui/material';
import { Add as AddIcon, People as AccountsIcon } from '@mui/icons-material';
import {
  createAccount, deleteAccount, setAccountGrants, setAccountRole,
} from '../../api/client.js';
import { useAuth } from '../../context/AuthContext.jsx';
import useAccounts from '../../hooks/useAccounts.js';

const ROLES = ['viewer', 'operator', 'admin'];
// NAME_WIDTH keeps every row's controls on one axis: the role select sits
// right after the name column and the grant row starts under it.
const NAME_WIDTH = 200;
const EMPTY_FORM = { name: '', password: '', role: 'viewer' };

export default function AccountsSection() {
  const { name: currentUserName, roleAtLeast } = useAuth();
  const isAdmin = roleAtLeast('admin');
  const { items, loading, error, setError, refresh } = useAccounts(isAdmin);
  const [form, setForm] = useState(EMPTY_FORM);
  const [grantDrafts, setGrantDrafts] = useState({});
  const [creating, setCreating] = useState(false);
  // pending tracks account names with a mutation in flight, so a row's
  // controls disable for the full runAction span (through its post-success
  // refresh) - without this, a second click on the same row before the
  // first mutation's refresh() lands would compute `next` from the same
  // stale account.grants/role snapshot and silently overwrite it.
  const [pending, setPending] = useState(() => new Set());
  const isPending = (name) => pending.has(name);
  const setRowPending = (name, busy) => {
    setPending((current) => {
      const next = new Set(current);
      if (busy) next.add(name); else next.delete(name);
      return next;
    });
  };

  if (!isAdmin) return null;

  const draftFor = (name) => grantDrafts[name] || { project: '', role: 'viewer' };
  const setDraft = (name, patch) => {
    setGrantDrafts((current) => ({ ...current, [name]: { ...draftFor(name), ...patch } }));
  };

  // runAction wraps every mutation's identical shape: clear the previous
  // error, run it, refresh the list on success, surface the message on
  // failure. No optimistic update - the list only reflects the change once
  // the request resolves (spec: "Backend rejection is surfaced").
  const runAction = async (action, onBusyChange) => {
    onBusyChange?.(true);
    setError(null);
    try {
      await action();
      await refresh();
    } catch (cause) {
      setError(cause.message);
    } finally {
      onBusyChange?.(false);
    }
  };

  const submitCreate = (event) => {
    event.preventDefault();
    return runAction(async () => {
      await createAccount(form);
      setForm(EMPTY_FORM);
    }, setCreating);
  };

  const changeRole = (account, role) => {
    const prompt = account.name === currentUserName
      ? `Change your own global role to "${role}"? This applies immediately and may end your access to this page.`
      : `Change ${account.name}'s global role to "${role}"?`;
    if (!window.confirm(prompt)) return undefined;
    return runAction(
      () => setAccountRole(account.name, role),
      (busy) => setRowPending(account.name, busy),
    );
  };

  const addGrant = (account) => {
    const project = draftFor(account.name).project.trim();
    if (!project) return undefined;
    const role = draftFor(account.name).role;
    const next = [
      ...(account.grants || []).filter((g) => g.project_name !== project),
      { project_name: project, role },
    ];
    return runAction(async () => {
      await setAccountGrants(account.name, next);
      setDraft(account.name, { project: '', role: 'viewer' });
    }, (busy) => setRowPending(account.name, busy));
  };

  const removeGrant = (account, projectName) => {
    const next = (account.grants || []).filter((g) => g.project_name !== projectName);
    return runAction(
      () => setAccountGrants(account.name, next),
      (busy) => setRowPending(account.name, busy),
    );
  };

  const remove = (account) => {
    if (!window.confirm(`Delete account "${account.name}"?`)) return undefined;
    return runAction(
      () => deleteAccount(account.name),
      (busy) => setRowPending(account.name, busy),
    );
  };

  return (
    <Card sx={{ mt: 3 }}>
      <CardContent>
        <Stack direction="row" spacing={1} sx={{ alignItems: 'center', mb: 2 }}>
          <AccountsIcon />
          <Box>
            <Typography component="h2" variant="h5">Accounts</Typography>
            <Typography variant="body2" color="text.secondary">
              Create, promote, grant, and remove accounts.
            </Typography>
          </Box>
        </Stack>

        {loading && <LinearProgress sx={{ mb: 2 }} />}
        {error && <Alert severity="error" sx={{ mb: 2 }}>{error}</Alert>}

        <Box component="form" onSubmit={submitCreate} sx={{ mb: 2 }}>
          <Stack direction={{ xs: 'column', sm: 'row' }} spacing={1} sx={{ alignItems: { sm: 'center' } }}>
            <TextField
              label="Name"
              size="small"
              required
              disabled={creating}
              value={form.name}
              onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))}
            />
            <TextField
              label="Password"
              size="small"
              type="password"
              required
              disabled={creating}
              value={form.password}
              onChange={(e) => setForm((f) => ({ ...f, password: e.target.value }))}
            />
            <FormControl size="small" sx={{ minWidth: 120 }} disabled={creating}>
              <InputLabel id="new-account-role-label">Role</InputLabel>
              <Select
                labelId="new-account-role-label"
                label="Role"
                value={form.role}
                onChange={(e) => setForm((f) => ({ ...f, role: e.target.value }))}
              >
                {ROLES.map((r) => <MenuItem key={r} value={r}>{r}</MenuItem>)}
              </Select>
            </FormControl>
            <Button type="submit" variant="contained" startIcon={<AddIcon />} disabled={creating} sx={{ whiteSpace: 'nowrap', flexShrink: 0 }}>Add account</Button>
          </Stack>
        </Box>

        {!loading && items.length === 0 && <Alert severity="info">No accounts yet.</Alert>}

        <List disablePadding>
          {items.map((account) => (
            <ListItem
              key={account.name}
              divider
              disableGutters
              data-testid={`account-row-${account.name}`}
              sx={{ display: 'block', py: 1.5 }}
            >
              <Stack direction={{ xs: 'column', sm: 'row' }} spacing={1} useFlexGap sx={{ alignItems: { sm: 'center' } }}>
                <ListItemText sx={{ minWidth: 0, my: 0, width: { sm: NAME_WIDTH }, flexGrow: 0, flexShrink: 0 }} primary={account.name} />
                <FormControl size="small" sx={{ minWidth: 120 }} disabled={isPending(account.name)}>
                  <InputLabel id={`account-role-label-${account.name}`}>Role</InputLabel>
                  <Select
                    labelId={`account-role-label-${account.name}`}
                    label="Role"
                    value={account.role}
                    onChange={(e) => changeRole(account, e.target.value)}
                  >
                    {ROLES.map((r) => <MenuItem key={r} value={r}>{r}</MenuItem>)}
                  </Select>
                </FormControl>
                <Button
                  size="small"
                  color="error"
                  disabled={isPending(account.name)}
                  onClick={() => remove(account)}
                  sx={{ ml: { sm: 'auto' }, alignSelf: { xs: 'flex-start', sm: 'center' } }}
                >
                  Delete
                </Button>
              </Stack>
              <Stack
                direction="row"
                spacing={1}
                useFlexGap
                sx={{ flexWrap: 'wrap', mt: 1, alignItems: 'center', ml: { sm: `${NAME_WIDTH + 8}px` } }}
              >
                {(account.grants || []).map((grant) => (
                  <Chip
                    key={grant.project_name}
                    label={`${grant.project_name}: ${grant.role}`}
                    disabled={isPending(account.name)}
                    onDelete={() => removeGrant(account, grant.project_name)}
                  />
                ))}
                <TextField
                  label="Project"
                  size="small"
                  disabled={isPending(account.name)}
                  value={draftFor(account.name).project}
                  onChange={(e) => setDraft(account.name, { project: e.target.value })}
                />
                <FormControl size="small" sx={{ minWidth: 120 }} disabled={isPending(account.name)}>
                  <InputLabel id={`grant-role-label-${account.name}`}>Grant role</InputLabel>
                  <Select
                    labelId={`grant-role-label-${account.name}`}
                    label="Grant role"
                    value={draftFor(account.name).role}
                    onChange={(e) => setDraft(account.name, { role: e.target.value })}
                  >
                    {ROLES.map((r) => <MenuItem key={r} value={r}>{r}</MenuItem>)}
                  </Select>
                </FormControl>
                <IconButton
                  size="small"
                  disabled={isPending(account.name)}
                  onClick={() => addGrant(account)}
                  aria-label={`Add grant for ${account.name}`}
                >
                  <AddIcon fontSize="small" />
                </IconButton>
              </Stack>
            </ListItem>
          ))}
        </List>
      </CardContent>
    </Card>
  );
}
