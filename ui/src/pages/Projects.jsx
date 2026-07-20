import { useState, useEffect, useCallback } from 'react';
import {
  Box, Typography, Card, CardContent, Grid, Chip, Table, TableBody,
  TableCell, TableContainer, TableHead, TableRow, Paper, IconButton,
  Tooltip, Alert, LinearProgress, TextField, Button, Divider, Stack
} from '@mui/material';
import {
  Folder as RepoIcon,
  PlayArrow as TaskIcon,
  Refresh as RefreshIcon,
  Add as AddIcon,
  AccountTree as PipelineIcon,
} from '@mui/icons-material';
import { listProjects, createProject } from '../api/client';

export default function Projects({ onOpenPipeline }) {
  const [projects, setProjects] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);

  // Create form
  const [name, setName] = useState('');
  const [repoPath, setRepoPath] = useState('');
  const [repoURL, setRepoURL] = useState('');
  const [creating, setCreating] = useState(false);
  const [formError, setFormError] = useState(null);

  const fetchProjects = useCallback(async () => {
    setLoading(true);
    try {
      const data = await listProjects();
      setProjects(data || []);
    } catch (e) {
      setError(e.message);
    }
    setLoading(false);
  }, []);

  useEffect(() => { fetchProjects(); }, [fetchProjects]);

  const handleCreate = async (e) => {
    e.preventDefault();
    setCreating(true);
    setFormError(null);
    try {
      await createProject({ name, repo_path: repoPath, repo_url: repoURL });
      setName(''); setRepoPath(''); setRepoURL('');
      await fetchProjects();
    } catch (err) {
      setFormError(err.message);
    }
    setCreating(false);
  };

  return (
    <Box>
      <Box sx={{ display: 'flex', alignItems: 'center', mb: 3 }}>
        <Typography variant="h4" sx={{ flexGrow: 1 }}>Projects</Typography>
        <Tooltip title="Refresh">
          <IconButton onClick={fetchProjects}><RefreshIcon /></IconButton>
        </Tooltip>
      </Box>

      {loading && <LinearProgress sx={{ mb: 2 }} />}
      {error && <Alert severity="error" sx={{ mb: 2 }}>{error}</Alert>}

      {/* Create project form */}
      <Card sx={{ mb: 3 }}>
        <CardContent>
          <Typography variant="h6" gutterBottom>
            <AddIcon sx={{ mr: 1, verticalAlign: 'middle' }} />
            New Project
          </Typography>
          <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
            Point MetaCI at a repository that contains (or will contain) <code>pipeline.tf</code>.
            For now use a local path; GitLab URL support is coming.
          </Typography>
          <Box component="form" onSubmit={handleCreate}>
            <Stack spacing={2}>
              <TextField
                label="Project name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                required
                size="small"
                placeholder="my-tf-plugin"
              />
              <TextField
                label="Repo path (local)"
                value={repoPath}
                onChange={(e) => setRepoPath(e.target.value)}
                size="small"
                placeholder="/home/user/repos/my-tf-plugin"
                helperText="Directory on this machine where pipeline.tf lives"
              />
              <TextField
                label="Repo URL (GitLab, optional — future)"
                value={repoURL}
                onChange={(e) => setRepoURL(e.target.value)}
                size="small"
                placeholder="https://gitlab.com/group/repo"
                disabled
              />
              {formError && <Alert severity="error">{formError}</Alert>}
              <Box>
                <Button type="submit" variant="contained" disabled={creating || !name}>
                  {creating ? 'Creating…' : 'Create Project'}
                </Button>
              </Box>
            </Stack>
          </Box>
        </CardContent>
      </Card>

      {/* Project list */}
      {projects.length === 0 && !loading && (
        <Alert severity="info">No projects yet. Create one above.</Alert>
      )}

      <Grid container spacing={3}>
        {projects.map(p => (
          <Grid item xs={12} md={6} key={p.name}>
            <Card>
              <CardContent>
                <Typography variant="h6" gutterBottom>📦 {p.name}</Typography>
                <Typography variant="caption" color="text.secondary" display="block" sx={{ mb: 1 }}>
                  {p.repo_path || p.repo_url}
                </Typography>
                <Stack direction="row" spacing={1}>
                  <Button
                    size="small"
                    variant="outlined"
                    startIcon={<PipelineIcon />}
                    onClick={() => onOpenPipeline && onOpenPipeline(p.name)}
                  >
                    Open Pipeline
                  </Button>
                </Stack>
              </CardContent>
            </Card>
          </Grid>
        ))}
      </Grid>
    </Box>
  );
}
