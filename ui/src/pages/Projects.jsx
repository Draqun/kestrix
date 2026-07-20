import { useState, useEffect } from 'react';
import {
  Box, Typography, Card, CardContent, Grid, Chip, Table, TableBody,
  TableCell, TableContainer, TableHead, TableRow, Paper, IconButton,
  Tooltip, Alert, LinearProgress
} from '@mui/material';
import {
  Folder as RepoIcon,
  PlayArrow as TaskIcon,
  Refresh as RefreshIcon,
} from '@mui/icons-material';
import { parsePipeline } from '../api/client';

// Client-side HCL parser (fallback when API unavailable)
function parseHCLFallback() {
  const example = `project "demo-mono" {
  default_image = "golang:1.26"

  ci_repo "auth_service" { path = "src/auth" kind = "subpath" }
  ci_repo "billing_service" { path = "src/billing" kind = "subpath" }
  ci_repo "docs" { path = "docs" }
  ci_repo "api_gateway" { path = "src/api-gateway" kind = "subpath" }

  ci_task "test_auth" {
    trigger = "repo.auth_service.changed"
    execute { repo = "auth_service" script = "make test" }
  }
  ci_task "test_billing" {
    trigger = "repo.billing_service.changed"
    execute { repo = "billing_service" script = "make test" }
  }
  ci_task "integration_tests" {
    trigger = "repo.auth_service.changed || repo.billing_service.changed && !repo.docs.changed"
    execute { repo = "api_gateway" script = "make test-integration" }
  }
  ci_task "deploy_docs" {
    trigger = "repo.docs.changed"
    execute { repo = "docs" script = "make docs-deploy" }
  }
}`;
  return example;
}

export default function Projects() {
  const [project, setProject] = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);

  const fetchProject = async () => {
    setLoading(true);
    try {
      const data = await parsePipeline();
      setProject(data);
    } catch (e) {
      // Fallback to client-side parsing
      try {
        const text = parseHCLFallback();
        // Simple client-side parse
        const repos = [];
        const tasks = [];
        const repoRegex = /ci_repo\s+"([^"]+)"\s*\{([^}]+)\}/g;
        const taskRegex = /ci_task\s+"([^"]+)"\s*\{([^}]+(?:\{[^}]*\}[^}]*)*)\}/g;
        let m;
        while ((m = repoRegex.exec(text)) !== null) {
          const attrs = {};
          m[2].trim().split(/\s+/).forEach(part => {
            const [k, v] = part.split('=');
            if (k && v) attrs[k] = v.replace(/"/g, '');
          });
          repos.push({ name: m[1], ...attrs });
        }
        while ((m = taskRegex.exec(text)) !== null) {
          const body = m[2];
          const triggerMatch = body.match(/trigger\s*=\s*"([^"]+)"/);
          const repoMatch = body.match(/repo\s*=\s*"([^"]+)"/);
          const scriptMatch = body.match(/script\s*=\s*"([^"]+)"/);
          tasks.push({
            name: m[1],
            trigger: triggerMatch?.[1] || '',
            repo: repoMatch?.[1] || '',
            script: scriptMatch?.[1] || '',
          });
        }
        setProject({ name: 'demo-mono', repos, tasks });
      } catch (e2) {
        setError(e.message);
      }
    }
    setLoading(false);
  };

  useEffect(() => { fetchProject(); }, []);

  return (
    <Box>
      <Box sx={{ display: 'flex', alignItems: 'center', mb: 3 }}>
        <Typography variant="h4" sx={{ flexGrow: 1 }}>Projects</Typography>
        <Tooltip title="Refresh">
          <IconButton onClick={fetchProject}><RefreshIcon /></IconButton>
        </Tooltip>
      </Box>

      {loading && <LinearProgress sx={{ mb: 2 }} />}
      {error && <Alert severity="error" sx={{ mb: 2 }}>{error}</Alert>}

      {project && (
        <>
          <Card sx={{ mb: 3 }}>
            <CardContent>
              <Typography variant="h5" gutterBottom>📦 {project.name}</Typography>
              <Box sx={{ display: 'flex', gap: 1 }}>
                <Chip label={`${project.repos?.length || 0} repos`} color="primary" />
                <Chip label={`${project.tasks?.length || 0} tasks`} color="secondary" />
              </Box>
            </CardContent>
          </Card>

          <Grid container spacing={3}>
            {/* Repos Table */}
            <Grid item xs={12} md={6}>
              <Card>
                <CardContent>
                  <Typography variant="h6" gutterBottom>
                    <RepoIcon sx={{ mr: 1, verticalAlign: 'middle' }} />
                    Repositories
                  </Typography>
                  <TableContainer>
                    <Table size="small">
                      <TableHead>
                        <TableRow>
                          <TableCell>Name</TableCell>
                          <TableCell>Path</TableCell>
                          <TableCell>Kind</TableCell>
                        </TableRow>
                      </TableHead>
                      <TableBody>
                        {project.repos?.map(repo => (
                          <TableRow key={repo.name}>
                            <TableCell><strong>{repo.name}</strong></TableCell>
                            <TableCell><code>{repo.path}</code></TableCell>
                            <TableCell>
                              <Chip label={repo.kind || 'git'} size="small"
                                color={repo.kind === 'subpath' ? 'primary' : 'default'} />
                            </TableCell>
                          </TableRow>
                        ))}
                      </TableBody>
                    </Table>
                  </TableContainer>
                </CardContent>
              </Card>
            </Grid>

            {/* Tasks Table */}
            <Grid item xs={12} md={6}>
              <Card>
                <CardContent>
                  <Typography variant="h6" gutterBottom>
                    <TaskIcon sx={{ mr: 1, verticalAlign: 'middle' }} />
                    Tasks
                  </Typography>
                  <TableContainer>
                    <Table size="small">
                      <TableHead>
                        <TableRow>
                          <TableCell>Name</TableCell>
                          <TableCell>Repo</TableCell>
                          <TableCell>Trigger</TableCell>
                        </TableRow>
                      </TableHead>
                      <TableBody>
                        {project.tasks?.map(task => (
                          <TableRow key={task.name}>
                            <TableCell><strong>{task.name}</strong></TableCell>
                            <TableCell><code>{task.repo}</code></TableCell>
                            <TableCell>
                              <code style={{ fontSize: 11 }}>{task.trigger}</code>
                            </TableCell>
                          </TableRow>
                        ))}
                      </TableBody>
                    </Table>
                  </TableContainer>
                </CardContent>
              </Card>
            </Grid>
          </Grid>
        </>
      )}
    </Box>
  );
}
