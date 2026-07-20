import { useState, useEffect, useCallback } from 'react';
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
} from '@mui/icons-material';
import { listProjects, createProject, deleteProject, getProjectDefinition } from '../api/client';

export default function Projects({ onOpenPipeline, selectedName, onSelect }) {
  const [projects, setProjects] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);

  const [detail, setDetail] = useState(null);
  const [detailLoading, setDetailLoading] = useState(false);

  const [showCreate, setShowCreate] = useState(false);
  const [name, setName] = useState('');
  const [repoPath, setRepoPath] = useState('');
  const [pipelineDir, setPipelineDir] = useState('ci');
  const [creating, setCreating] = useState(false);
  const [formError, setFormError] = useState(null);

  const fetchProjects = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const data = await listProjects();
      setProjects(data || []);
    } catch (e) {
      setError(e.message);
    }
    setLoading(false);
  }, []);

  useEffect(() => { fetchProjects(); }, [fetchProjects]);

  const selected = selectedName ? projects.find(p => p.name === selectedName) || null : null;

  useEffect(() => {
    if (!selected) { setDetail(null); return; }
    setDetailLoading(true);
    getProjectDefinition(selected.name)
      .then(d => setDetail(d))
      .catch(() => setDetail(null))
      .finally(() => setDetailLoading(false));
  }, [selected]);

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

  if (selected) {
    return (
      <Box>
        <Box sx={{ display: 'flex', alignItems: 'center', mb: 3 }}>
          <Button size="small" startIcon={<BackIcon />} onClick={() => onSelect(null)}>
            Back to list
          </Button>
          <Typography variant="h4" sx={{ ml: 2 }}>{selected.name}</Typography>
          <Chip label={selected.pipeline_dir || 'ci'} size="small" variant="outlined" sx={{ ml: 1 }} />
          <Box sx={{ flexGrow: 1 }} />
                <Tooltip title="Open pipeline">
                  <IconButton size="small" onClick={(e) => { e.stopPropagation(); onOpenPipeline && onOpenPipeline(p.name); }}>
                    <PipelineIcon fontSize="small" />
                  </IconButton>
                </Tooltip>
                <Tooltip title="Delete project">
            <IconButton color="error" onClick={() => handleDelete(selected.name)}>
              <DeleteIcon />
            </IconButton>
          </Tooltip>
        </Box>

        {detailLoading && <LinearProgress sx={{ mb: 2 }} />}

        <Card sx={{ mb: 2 }}>
          <CardContent>
            <Typography variant="subtitle2" color="text.secondary" gutterBottom>Repository path</Typography>
            <Typography variant="body1" sx={{ mb: 2 }}>{selected.repo_path || selected.repo_url || 'Not configured'}</Typography>

            <Typography variant="subtitle2" color="text.secondary" gutterBottom>Pipeline directory</Typography>
            <Typography variant="body1" sx={{ mb: 2 }}>{selected.pipeline_dir || 'ci'}</Typography>

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
                        color={t.Manual ? 'warning' : 'success'}
                        variant="outlined"
                      />
                      {t.Manual && <Chip label="Manual" size="small" variant="filled" color="warning" sx={{ fontSize: 10 }} />}
                      {t.Trigger && <Typography variant="caption" color="text.secondary">{t.Trigger}</Typography>}
                    </Box>
                  )) || <Typography variant="body2" color="text.secondary">None</Typography>}
                </Stack>
              </>
            )}
          </CardContent>
        </Card>
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

      <Dialog open={showCreate} onClose={() => setShowCreate(false)} maxWidth="sm" fullWidth>
        <DialogTitle>New project</DialogTitle>
        <DialogContent>
          <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
            Point MetaCI at a local repository and the directory containing its pipeline files.
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
