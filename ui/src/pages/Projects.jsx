import { useState, useEffect, useCallback } from 'react';
import { useSearchParams } from 'react-router-dom';
import {
  Box, Typography, IconButton, Tooltip, Alert,
  LinearProgress, TextField, Button, Stack,
  Dialog, DialogTitle, DialogContent, DialogActions,
  List, ListItem, ListItemText, ListItemIcon, ListItemSecondaryAction,
  Divider, Chip, Card, CardContent, InputAdornment,
} from '@mui/material';
import {
  Refresh as RefreshIcon,
  Add as AddIcon,
  Delete as DeleteIcon,
  Folder as ProjectIcon,
  AccountTree as PipelineIcon,
  ArrowBack as BackIcon,
  Edit as EditIcon,
  Visibility as ShowIcon,
  VisibilityOff as HideIcon,
} from '@mui/icons-material';
import { listProjects, createProject, deleteProject, getProject, updateProject, getProjectDefinition } from '../api/client';
import CredentialSelectField from '../components/credentials/CredentialSelectField.jsx';
import { useAuth } from '../context/AuthContext';
import Pagination from '../components/Pagination';
import WebhookSetupPanel from '../components/projects/WebhookSetupPanel.jsx';
import useCredentials from '../hooks/useCredentials.js';
import {
  remoteProjectPayload,
  repositorySourceLabel,
  validateRemoteProject,
} from '../lib/projectForm.js';

// Shared by both dialogs. A stored secret is never read back from the API,
// so this field always starts empty. An empty value means different things
// per dialog: on create it means "no per-project secret, use the global
// one"; on edit it means "keep the stored secret unchanged".
function WebhookSecretField({ value, onChange, helperText }) {
  const [reveal, setReveal] = useState(false);
  return (
    <TextField
      label="Webhook secret (optional)"
      type={reveal ? 'text' : 'password'}
      value={value}
      onChange={(e) => onChange(e.target.value)}
      size="small"
      fullWidth
      autoComplete="new-password"
      helperText={helperText}
      slotProps={{
        input: {
          endAdornment: (
            <InputAdornment position="end">
              <IconButton
                size="small"
                aria-label={reveal ? 'Hide webhook secret' : 'Show webhook secret'}
                onClick={() => setReveal(prev => !prev)}
                edge="end"
              >
                {reveal ? <HideIcon fontSize="small" /> : <ShowIcon fontSize="small" />}
              </IconButton>
            </InputAdornment>
          ),
        },
      }}
    />
  );
}

export default function Projects({ onOpenPipeline, selectedName, onSelect }) {
  const { roleAtLeast } = useAuth();
  const canAdminister = roleAtLeast('admin');
  const [searchParams, setSearchParams] = useSearchParams();
  const allowedLimits = [10, 25, 50, 100];
  const limit = allowedLimits.includes(Number(searchParams.get('limit')))
    ? Number(searchParams.get('limit'))
    : 25;
  const page = Math.max(1, Number(searchParams.get('page')) || 1);
  const offset = (page - 1) * limit;

  const [projects, setProjects] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);
  const [pagination, setPagination] = useState(null);

  const [config, setConfig] = useState(null);
  const [detail, setDetail] = useState(null);
  const [detailLoading, setDetailLoading] = useState(false);

  const [showCreate, setShowCreate] = useState(false);
  const [showEdit, setShowEdit] = useState(false);
  const [editForm, setEditForm] = useState({ repo_url: '', repo_ref: '', repo_credential_ref: '', pipeline_dir: 'ci', pipeline_file: 'pipeline.tf', webhook_secret: '' });
  const [saving, setSaving] = useState(false);

  const [name, setName] = useState('');
	const [repoURL, setRepoURL] = useState('');
	const [repoRef, setRepoRef] = useState('main');
	const [credentialRef, setCredentialRef] = useState('');
  const [pipelineDir, setPipelineDir] = useState('ci');
  const [pipelineFile, setPipelineFile] = useState('pipeline.tf');
  const [webhookSecret, setWebhookSecret] = useState('');
  const [creating, setCreating] = useState(false);
  const [formError, setFormError] = useState(null);
	const { items: credentials, refresh: fetchCredentials } = useCredentials();

  const fetchProjects = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const resp = await listProjects(limit, offset);
      setProjects(resp.items || []);
      setPagination(resp.pagination);
    } catch (e) {
      setError(e.message);
    }
    setLoading(false);
  }, [limit, offset]);

  useEffect(() => { fetchProjects(); }, [fetchProjects]);

  const handlePageChange = (newOffset) => {
    const newPage = Math.floor(newOffset / limit) + 1;
    setSearchParams(prev => {
      const next = new URLSearchParams(prev);
      next.set('page', String(newPage));
      next.set('limit', String(limit));
      return next;
    });
  };

  const handleLimitChange = (newLimit) => {
    setSearchParams(prev => {
      const next = new URLSearchParams(prev);
      next.set('limit', String(newLimit));
      next.set('page', '1');
      return next;
    });
  };

  useEffect(() => {
    if (!selectedName) { setConfig(null); setDetail(null); return; }
    setDetailLoading(true);
    // The definition route re-derives the project's sources as a side effect,
    // so it has to finish before the config is read. Running both in parallel
    // rendered the source list from before the refresh, and nothing re-read it.
    getProjectDefinition(selectedName)
      .catch(() => null)
      .then(async (def) => {
        const cfg = await getProject(selectedName).catch(() => null);
        setConfig(cfg);
        setDetail(def);
      })
      .finally(() => setDetailLoading(false));
  }, [selectedName]);

  // A leftover webhookSecret from a cancelled or dismissed dialog would
  // otherwise still be sitting in state the next time "Add project" opens,
  // and remoteProjectPayload would silently send project A's secret for
  // project B. Resetting is therefore not just tidiness on success - it has
  // to run on every way the dialog closes without saving.
  const resetCreateForm = () => {
    setName(''); setRepoURL(''); setRepoRef('main'); setCredentialRef(''); setPipelineDir('ci');
    setPipelineFile('pipeline.tf'); setWebhookSecret(''); setFormError(null);
  };

  const closeCreateDialog = () => {
    resetCreateForm();
    setShowCreate(false);
  };

  const handleCreate = async (e) => {
    e.preventDefault();
    setCreating(true);
    setFormError(null);
	try {
		const form = {
			repo_url: repoURL, repo_ref: repoRef, repo_credential_ref: credentialRef,
			pipeline_dir: pipelineDir, pipeline_file: pipelineFile, webhook_secret: webhookSecret,
		};
		const validationError = validateRemoteProject(name, form);
		if (validationError) throw new Error(validationError);
		await createProject(remoteProjectPayload(name, form));
		resetCreateForm();
      setShowCreate(false);
      await fetchProjects();
    } catch (err) {
      setFormError(err.message);
    }
    setCreating(false);
  };

  const handleDelete = async (projName) => {
    if (!window.confirm(`Delete project "${projName}"?`)) return;
    try {
      await deleteProject(projName);
      if (selectedName === projName) onSelect(null);
      await fetchProjects();
    } catch (err) {
      setError(err.message);
    }
  };

  const openEdit = () => {
    setEditForm({
      repo_url: config?.repo_url || '',
      repo_ref: config?.repo_ref || '',
      repo_credential_ref: config?.repo_credential_ref || '',
      pipeline_dir: config?.pipeline_dir || 'ci',
      pipeline_file: config?.pipeline_file || 'pipeline.tf',
      // Deliberately blank: the API never returns the stored secret, and an
      // empty value is what tells remoteProjectPayload to leave it alone.
      webhook_secret: '',
    });
    setShowEdit(true);
  };

  const handleSaveEdit = async () => {
    setSaving(true);
    setError(null);
    try {
		const validationError = validateRemoteProject(undefined, editForm);
		if (validationError) throw new Error(validationError);
		await updateProject(selectedName, remoteProjectPayload(undefined, editForm));
		const [validatedConfig, validatedDefinition] = await Promise.all([
			getProject(selectedName), getProjectDefinition(selectedName),
		]);
		setConfig(validatedConfig);
		setDetail(validatedDefinition);
      setShowEdit(false);
    } catch (e) {
      setError(e.message);
    }
    setSaving(false);
  };

  if (selectedName) {
    return (
      <Box>
        <Box sx={{ display: 'flex', alignItems: 'center', mb: 3 }}>
          <Button size="small" startIcon={<BackIcon />} onClick={() => onSelect(null)}>
            Back to list
          </Button>
          <Typography variant="h4" sx={{ ml: 2 }}>{selectedName}</Typography>
          <Chip label={config?.pipeline_dir || 'ci'} size="small" variant="outlined" sx={{ ml: 1 }} />
          <Chip
            data-testid="webhook-secret-chip"
            label={config?.has_webhook_secret ? 'Secret set' : 'No per-project secret — uses global secret'}
            size="small"
            variant="outlined"
            color={config?.has_webhook_secret ? 'success' : 'warning'}
            sx={{ ml: 1 }}
          />
          <Box sx={{ flexGrow: 1 }} />
          {canAdminister && (
            <Tooltip title="Edit project">
              <IconButton size="small" onClick={openEdit}>
                <EditIcon fontSize="small" />
              </IconButton>
            </Tooltip>
          )}
          <Tooltip title="Open pipeline">
            <IconButton size="small" onClick={() => onOpenPipeline && onOpenPipeline(selectedName)}>
              <PipelineIcon fontSize="small" />
            </IconButton>
          </Tooltip>
          {canAdminister && (
            <Tooltip title="Delete project">
              <IconButton color="error" onClick={() => handleDelete(selectedName)}>
                <DeleteIcon />
              </IconButton>
            </Tooltip>
          )}
        </Box>

        {detailLoading && <LinearProgress sx={{ mb: 2 }} />}
        {error && <Alert severity="error" sx={{ mb: 2 }}>{error}</Alert>}

        {/* Detail cards */}
        <Card sx={{ mb: 2 }}>
          <CardContent>
			<Typography variant="subtitle2" color="text.secondary" gutterBottom>Remote repository</Typography>
			<Typography variant="body1">{config?.repo_url || 'Not configured'}</Typography>
			<Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
				Requested ref: {config?.repo_ref || 'Not configured'}
				{config?.repo_credential_ref ? ` · Credential: ${credentials.find(item => item.ref === config.repo_credential_ref)?.name || config.repo_credential_ref}` : ' · Public access'}
			</Typography>

            <Typography variant="subtitle2" color="text.secondary" gutterBottom>Pipeline directory</Typography>
            <Typography variant="body1" sx={{ mb: 2 }}>
              {config?.pipeline_dir || 'ci'} / {config?.pipeline_file || 'pipeline.tf'}
            </Typography>

            {/* Sources are derived from repo_url and the pipeline's remote ci_repo blocks; a push to any of them reaches this project. */}
            <Typography variant="subtitle2" color="text.secondary" gutterBottom>Sources ({config?.sources?.length ?? 0})</Typography>
            <Stack spacing={0.5} sx={{ mb: 2 }} data-testid="project-sources">
              {(config?.sources || []).map(src => (
                <Box key={src.url} data-testid={`project-source-${src.url}`} sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                  <Typography variant="body2">{src.name}</Typography>
                  <Typography variant="body2" color="text.secondary">{src.url}</Typography>
                  {src.primary && <Chip label="Primary" size="small" variant="outlined" color="primary" sx={{ fontSize: 10 }} />}
                </Box>
              ))}
              {(config?.sources || []).length === 0 && (
                <Typography variant="body2" color="text.secondary">Derived on the next pipeline load.</Typography>
              )}
            </Stack>

            {detail && (
              <>
                <Divider sx={{ my: 2 }} />
                <Typography variant="subtitle2" color="text.secondary" gutterBottom>Repositories ({detail.Repos?.length ?? 0})</Typography>
                <Stack direction="row" spacing={1} sx={{ mb: 2, flexWrap: 'wrap', gap: 1 }}>
				  {detail.Repos?.map(r => (
					<Chip key={r.Name} label={repositorySourceLabel(r)} size="small" variant="outlined" />
                  )) || <Typography variant="body2" color="text.secondary">None</Typography>}
                </Stack>

                <Typography variant="subtitle2" color="text.secondary" gutterBottom>Tasks ({detail.Tasks?.length ?? 0})</Typography>
                <Stack spacing={1}>
                  {detail.Tasks?.map(t => (
                    <Box key={t.Name} sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                      <Chip
                        label={t.Name}
                        size="small"
                        color={t.Manual ? 'info' : 'success'}
                        variant="outlined"
                      />
                      {t.Manual && <Chip label="Manual" size="small" variant="filled" color="info" sx={{ fontSize: 10 }} />}
                      {t.Trigger && <Typography variant="caption" color="text.secondary">{t.Trigger}</Typography>}
                    </Box>
                  )) || <Typography variant="body2" color="text.secondary">None</Typography>}
                </Stack>
              </>
            )}
          </CardContent>
        </Card>

        {/* Repository declarations are owned by pipeline HCL. */}
        <Card>
          <CardContent>
			<Typography variant="h6" sx={{ mb: 1 }}>Pipeline repositories</Typography>
            <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
			  Read-only declarations loaded from the selected remote revision. Edit <code>ci_repo</code> blocks in Git to change them.
            </Typography>
			{(detail?.Repos || []).length === 0 && (
              <Typography variant="body2" color="text.secondary">
				No repositories declared in the pipeline.
              </Typography>
            )}
			<Stack spacing={1}>{detail?.Repos?.map(repo => (
				<Box key={repo.Name} sx={{ p: 1.5, border: 1, borderColor: 'divider', borderRadius: 1 }}>
					<Typography variant="subtitle2">{repo.Name}</Typography>
					<Typography variant="body2" color="text.secondary">
						{repo.URL ? `${repo.URL} @ ${repo.Ref}${repo.CredentialRef ? ` · credential ${repo.CredentialRef}` : ''}` : `Subpath: ${repo.Path}`}
					</Typography>
				</Box>
			))}</Stack>
          </CardContent>
        </Card>

        <WebhookSetupPanel hasProjectSecret={Boolean(config?.has_webhook_secret)} />

        {/* Edit project dialog */}
        <Dialog open={showEdit} onClose={() => setShowEdit(false)} maxWidth="sm" fullWidth>
          <DialogTitle>Edit project</DialogTitle>
          <DialogContent>
            <Stack spacing={2} sx={{ mt: 1 }}>
              <TextField
				label="Repository URL"
                value={editForm.repo_url}
                onChange={(e) => setEditForm(prev => ({ ...prev, repo_url: e.target.value }))}
                size="small"
				fullWidth
				required
                placeholder="https://gitlab.com/group/repo"
              />
              <TextField
				label="Repository ref"
                value={editForm.repo_ref}
                onChange={(e) => setEditForm(prev => ({ ...prev, repo_ref: e.target.value }))}
                size="small"
				fullWidth
				required
                placeholder="main, v1.2.3, or commit SHA"
              />
			  <CredentialSelectField
				value={editForm.repo_credential_ref}
				onChange={(value) => setEditForm(prev => ({ ...prev, repo_credential_ref: value }))}
				credentials={credentials}
				onCredentialsChanged={fetchCredentials}
			  />
              <TextField
                label="Pipeline directory"
                value={editForm.pipeline_dir}
                onChange={(e) => setEditForm(prev => ({ ...prev, pipeline_dir: e.target.value }))}
                size="small"
                fullWidth
                helperText="Relative to the repository; all *.tf files in this directory form one pipeline"
              />
              <TextField
                label="Pipeline file"
                value={editForm.pipeline_file}
                onChange={(e) => setEditForm(prev => ({ ...prev, pipeline_file: e.target.value }))}
                size="small"
                fullWidth
                placeholder="pipeline.tf"
                helperText="Entry file inside the pipeline directory"
              />
              <WebhookSecretField
                value={editForm.webhook_secret}
                onChange={(value) => setEditForm(prev => ({ ...prev, webhook_secret: value }))}
                helperText="Leave empty to keep the stored secret. Generate one with openssl rand -hex 32."
              />
            </Stack>
          </DialogContent>
          <DialogActions>
            <Button onClick={() => setShowEdit(false)}>Cancel</Button>
			<Button variant="contained" onClick={handleSaveEdit} disabled={saving || !editForm.repo_url || !editForm.repo_ref || !editForm.pipeline_dir}>
              {saving ? 'Saving…' : 'Save'}
            </Button>
          </DialogActions>
        </Dialog>
      </Box>
    );
  }

  return (
    <Box>
      <Box sx={{ display: 'flex', alignItems: 'center', mb: 3 }}>
        <Typography variant="h4" sx={{ flexGrow: 1 }}>Projects</Typography>
        <Tooltip title="Refresh">
          <IconButton onClick={fetchProjects}><RefreshIcon /></IconButton>
        </Tooltip>
        {canAdminister && (
          <Button variant="contained" startIcon={<AddIcon />} onClick={() => setShowCreate(true)} sx={{ ml: 1 }}>
            Add project
          </Button>
        )}
      </Box>

      {loading && <LinearProgress sx={{ mb: 2 }} />}
      {error && <Alert severity="error" sx={{ mb: 2 }}>{error}</Alert>}

      {projects.length === 0 && !loading && (
        <Alert severity="info">No projects yet. Click "Add project" to create one.</Alert>
      )}

      <List>
        {projects.map((p, i) => (
          <Box key={p.name}>
            {i > 0 && <Divider />}
            <ListItem
              sx={{ cursor: 'pointer', '&:hover': { bgcolor: 'action.hover' } }}
              onClick={() => onSelect(p.name)}
            >
              <ListItemIcon><ProjectIcon /></ListItemIcon>
              <ListItemText
                primary={
                  <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                    <Typography variant="subtitle1" fontWeight={600}>{p.name}</Typography>
                    <Chip label={p.pipeline_dir || 'ci'} size="small" variant="outlined" />
                  </Box>
                }
				secondary={`${p.repo_url || 'No remote configured'}${p.repo_ref ? ` @ ${p.repo_ref}` : ''}`}
              />
              <ListItemSecondaryAction>
                <Tooltip title="Open pipeline">
                  <IconButton size="small" onClick={(e) => { e.stopPropagation(); onOpenPipeline && onOpenPipeline(p.name); }}>
                    <PipelineIcon fontSize="small" />
                  </IconButton>
                </Tooltip>
                {canAdminister && (
                  <Tooltip title="Delete project">
                    <IconButton size="small" color="error" onClick={(e) => { e.stopPropagation(); handleDelete(p.name); }}>
                      <DeleteIcon fontSize="small" />
                    </IconButton>
                  </Tooltip>
                )}
              </ListItemSecondaryAction>
            </ListItem>
          </Box>
        ))}
      </List>

        {pagination && (
          <Pagination
            pagination={pagination}
            onPageChange={handlePageChange}
            onLimitChange={handleLimitChange}
          />
        )}

      <Dialog open={showCreate} onClose={closeCreateDialog} maxWidth="sm" fullWidth>
        <DialogTitle>New project</DialogTitle>
        <DialogContent>
          <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
			Register a remote Git repository. Kestrix validates the selected revision and pipeline before saving the project.
          </Typography>
          <Stack spacing={2} sx={{ mt: 1 }}>
            <TextField
              label="Project name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              required
              size="small"
              placeholder="my-project"
              autoFocus
              fullWidth
            />
            <TextField
			  label="Repository URL"
			  value={repoURL}
			  onChange={(e) => setRepoURL(e.target.value)}
			  size="small"
			  placeholder="https://gitlab.com/group/project.git"
			  helperText="Do not include a token or password in the URL"
			  required
			  fullWidth
			/>
			<TextField label="Repository ref" value={repoRef} onChange={(e) => setRepoRef(e.target.value)} size="small" placeholder="main, v1.2.3, or commit SHA" required fullWidth />
			<CredentialSelectField
			  value={credentialRef}
			  onChange={setCredentialRef}
			  credentials={credentials}
			  onCredentialsChanged={fetchCredentials}
			  helperText="Use public access or add a write-only credential for a private repository."
			/>
            <TextField
              label="Pipeline directory"
              value={pipelineDir}
              onChange={(e) => setPipelineDir(e.target.value)}
              required
              size="small"
              placeholder="ci"
              helperText="Relative to the repository; all *.tf files in this directory form one pipeline"
              fullWidth
            />
            <TextField
              label="Pipeline file"
              value={pipelineFile}
              onChange={(e) => setPipelineFile(e.target.value)}
              size="small"
              placeholder="pipeline.tf"
              helperText="Entry file inside the pipeline directory"
              fullWidth
            />
            <WebhookSecretField
              value={webhookSecret}
              onChange={setWebhookSecret}
              helperText="Verifies webhook deliveries for this project. Empty means the global secret is used. Generate one with openssl rand -hex 32."
            />
            {formError && <Alert severity="error">{formError}</Alert>}
          </Stack>
        </DialogContent>
        <DialogActions>
          <Button onClick={closeCreateDialog}>Cancel</Button>
		  <Button variant="contained" onClick={handleCreate} disabled={creating || !name || !repoURL || !repoRef || !pipelineDir}>
            {creating ? 'Creating…' : 'Create project'}
          </Button>
        </DialogActions>
      </Dialog>
    </Box>
  );
}
