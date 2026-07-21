import { useState, useEffect, useCallback } from 'react';
import { useSearchParams } from 'react-router-dom';
import {
  Box, Typography, IconButton, Tooltip, Alert,
  LinearProgress, TextField, Button, Stack,
  Dialog, DialogTitle, DialogContent, DialogActions,
  List, ListItem, ListItemText, ListItemIcon, ListItemSecondaryAction,
  Divider, Chip, Card, CardContent,
} from '@mui/material';
import {
  Refresh as RefreshIcon,
  Add as AddIcon,
  Delete as DeleteIcon,
  Folder as ProjectIcon,
  AccountTree as PipelineIcon,
  ArrowBack as BackIcon,
  Edit as EditIcon,
  Save as SaveIcon,
} from '@mui/icons-material';
import { listProjects, createProject, deleteProject, getProject, updateProject, getProjectDefinition } from '../api/client';
import Pagination from '../components/Pagination';

export default function Projects({ onOpenPipeline, selectedName, onSelect }) {
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
  const [editForm, setEditForm] = useState({ repo_path: '', repo_url: '', pipeline_dir: 'ci' });
  const [saving, setSaving] = useState(false);

  const [name, setName] = useState('');
  const [repoPath, setRepoPath] = useState('');
  const [pipelineDir, setPipelineDir] = useState('ci');
  const [creating, setCreating] = useState(false);
  const [formError, setFormError] = useState(null);

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
    Promise.all([
      getProject(selectedName).catch(() => null),
      getProjectDefinition(selectedName).catch(() => null),
    ]).then(([cfg, def]) => {
      setConfig(cfg);
      setDetail(def);
    }).finally(() => setDetailLoading(false));
  }, [selectedName]);

  const handleCreate = async (e) => {
    e.preventDefault();
    setCreating(true);
    setFormError(null);
    try {
      await createProject({ name, repo_path: repoPath, pipeline_dir: pipelineDir });
      setName(''); setRepoPath(''); setPipelineDir('ci');
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
      repo_path: config?.repo_path || '',
      repo_url: config?.repo_url || '',
      pipeline_dir: config?.pipeline_dir || 'ci',
    });
    setShowEdit(true);
  };

  const handleSaveEdit = async () => {
    setSaving(true);
    setError(null);
    try {
      const updated = { ...config, ...editForm };
      await updateProject(selectedName, updated);
      setConfig(updated);
      setShowEdit(false);
    } catch (e) {
      setError(e.message);
    }
    setSaving(false);
  };

  const handleAddRepo = async () => {
    const updated = {
      ...config,
      extra_repos: [...(config.extra_repos || []), { name: '', path: '', url: '', kind: 'subpath' }],
    };
    try {
      await updateProject(selectedName, updated);
      setConfig(updated);
    } catch (e) {
      setError(e.message);
    }
  };

  const handleRemoveRepo = async (idx) => {
    const updated = {
      ...config,
      extra_repos: config.extra_repos.filter((_, i) => i !== idx),
    };
    try {
      await updateProject(selectedName, updated);
      setConfig(updated);
    } catch (e) {
      setError(e.message);
    }
  };

  const handleRepoChange = (idx, field, value) => {
    setConfig(prev => ({
      ...prev,
      extra_repos: prev.extra_repos.map((r, i) => i === idx ? { ...r, [field]: value } : r),
    }));
  };

  const handleSaveRepos = async () => {
    setSaving(true);
    setError(null);
    try {
      await updateProject(selectedName, config);
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
          <Box sx={{ flexGrow: 1 }} />
          <Tooltip title="Edit project">
            <IconButton size="small" onClick={openEdit}>
              <EditIcon fontSize="small" />
            </IconButton>
          </Tooltip>
          <Tooltip title="Open pipeline">
            <IconButton size="small" onClick={() => onOpenPipeline && onOpenPipeline(selectedName)}>
              <PipelineIcon fontSize="small" />
            </IconButton>
          </Tooltip>
          <Tooltip title="Delete project">
            <IconButton color="error" onClick={() => handleDelete(selectedName)}>
              <DeleteIcon />
            </IconButton>
          </Tooltip>
        </Box>

        {detailLoading && <LinearProgress sx={{ mb: 2 }} />}
        {error && <Alert severity="error" sx={{ mb: 2 }}>{error}</Alert>}

        {/* Detail cards */}
        <Card sx={{ mb: 2 }}>
          <CardContent>
            <Typography variant="subtitle2" color="text.secondary" gutterBottom>Repository path</Typography>
            <Typography variant="body1" sx={{ mb: 2 }}>{config?.repo_path || config?.repo_url || 'Not configured'}</Typography>

            <Typography variant="subtitle2" color="text.secondary" gutterBottom>Pipeline directory</Typography>
            <Typography variant="body1" sx={{ mb: 2 }}>{config?.pipeline_dir || 'ci'}</Typography>

            {detail && (
              <>
                <Divider sx={{ my: 2 }} />
                <Typography variant="subtitle2" color="text.secondary" gutterBottom>Repositories ({detail.Repos?.length ?? 0})</Typography>
                <Stack direction="row" spacing={1} sx={{ mb: 2, flexWrap: 'wrap', gap: 1 }}>
                  {detail.Repos?.map(r => (
                    <Chip key={r.Name} label={r.Name} size="small" variant="outlined" />
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

        {/* Additional repos */}
        <Card>
          <CardContent>
            <Box sx={{ display: 'flex', alignItems: 'center', mb: 2 }}>
              <Typography variant="h6" sx={{ flexGrow: 1 }}>Additional repositories</Typography>
              <Button size="small" startIcon={<AddIcon />} onClick={handleAddRepo}>
                Add repo
              </Button>
            </Box>
            <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
              Extra repositories that participate in this project's pipeline.
              Define them here, then reference in your pipeline.tf as ci_repo blocks.
            </Typography>
            {(config?.extra_repos || []).length === 0 && (
              <Typography variant="body2" color="text.secondary">
                No additional repos configured.
              </Typography>
            )}
            <Stack spacing={2}>
              {(config?.extra_repos || []).map((repo, idx) => (
                <Card key={idx} variant="outlined" sx={{ p: 2 }}>
                  <Box sx={{ display: 'flex', alignItems: 'center', mb: 1 }}>
                    <Typography variant="subtitle2" sx={{ flexGrow: 1 }}>{repo.name || `Repo ${idx + 1}`}</Typography>
                    <IconButton size="small" color="error" onClick={() => handleRemoveRepo(idx)}>
                      <DeleteIcon fontSize="small" />
                    </IconButton>
                  </Box>
                  <Stack spacing={1.5}>
                    <TextField
                      label="Name"
                      value={repo.name}
                      onChange={(e) => handleRepoChange(idx, 'name', e.target.value)}
                      size="small"
                      fullWidth
                      placeholder="auth_service"
                      helperText="Local identifier used in trigger expressions"
                    />
                    <TextField
                      label="Path"
                      value={repo.path}
                      onChange={(e) => handleRepoChange(idx, 'path', e.target.value)}
                      size="small"
                      fullWidth
                      placeholder="src/auth or https://gitlab.com/org/auth.git"
                      helperText="Subdirectory within monorepo or clone URL"
                    />
                    <TextField
                      label="URL (optional)"
                      value={repo.url || ''}
                      onChange={(e) => handleRepoChange(idx, 'url', e.target.value)}
                      size="small"
                      fullWidth
                      placeholder="https://gitlab.com/group/repo"
                    />
                  </Stack>
                </Card>
              ))}
            </Stack>
            {(config?.extra_repos || []).length > 0 && (
              <Box sx={{ display: 'flex', justifyContent: 'flex-end', mt: 2 }}>
                <Button variant="contained" startIcon={<SaveIcon />} onClick={handleSaveRepos} disabled={saving}>
                  {saving ? 'Saving…' : 'Save repos'}
                </Button>
              </Box>
            )}
          </CardContent>
        </Card>

        {/* Edit project dialog */}
        <Dialog open={showEdit} onClose={() => setShowEdit(false)} maxWidth="sm" fullWidth>
          <DialogTitle>Edit project</DialogTitle>
          <DialogContent>
            <Stack spacing={2} sx={{ mt: 1 }}>
              <TextField
                label="Repo path (local)"
                value={editForm.repo_path}
                onChange={(e) => setEditForm(prev => ({ ...prev, repo_path: e.target.value }))}
                size="small"
                fullWidth
                helperText="Directory on this machine containing the source checkout"
              />
              <TextField
                label="Repository URL (optional)"
                value={editForm.repo_url}
                onChange={(e) => setEditForm(prev => ({ ...prev, repo_url: e.target.value }))}
                size="small"
                fullWidth
                placeholder="https://gitlab.com/group/repo"
              />
              <TextField
                label="Pipeline directory"
                value={editForm.pipeline_dir}
                onChange={(e) => setEditForm(prev => ({ ...prev, pipeline_dir: e.target.value }))}
                size="small"
                fullWidth
                helperText="Relative to the repository; all *.tf files in this directory form one pipeline"
              />
            </Stack>
          </DialogContent>
          <DialogActions>
            <Button onClick={() => setShowEdit(false)}>Cancel</Button>
            <Button variant="contained" onClick={handleSaveEdit} disabled={saving}>
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
        <Button variant="contained" startIcon={<AddIcon />} onClick={() => setShowCreate(true)} sx={{ ml: 1 }}>
          Add project
        </Button>
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
                secondary={p.repo_path || p.repo_url || 'No path configured'}
              />
              <ListItemSecondaryAction>
                <Tooltip title="Open pipeline">
                  <IconButton size="small" onClick={(e) => { e.stopPropagation(); onOpenPipeline && onOpenPipeline(p.name); }}>
                    <PipelineIcon fontSize="small" />
                  </IconButton>
                </Tooltip>
                <Tooltip title="Delete project">
                  <IconButton size="small" color="error" onClick={(e) => { e.stopPropagation(); handleDelete(p.name); }}>
                    <DeleteIcon fontSize="small" />
                  </IconButton>
                </Tooltip>
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

      <Dialog open={showCreate} onClose={() => setShowCreate(false)} maxWidth="sm" fullWidth>
        <DialogTitle>New project</DialogTitle>
        <DialogContent>
          <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
            Point Kestrix at a local repository and the directory containing its pipeline files.
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
              label="Repo path (local)"
              value={repoPath}
              onChange={(e) => setRepoPath(e.target.value)}
              size="small"
              placeholder="/home/user/repos/my-project"
              helperText="Directory on this machine containing the source checkout"
              fullWidth
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
            {formError && <Alert severity="error">{formError}</Alert>}
          </Stack>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setShowCreate(false)}>Cancel</Button>
          <Button variant="contained" onClick={handleCreate} disabled={creating || !name}>
            {creating ? 'Creating…' : 'Create project'}
          </Button>
        </DialogActions>
      </Dialog>
    </Box>
  );
}
