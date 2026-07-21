import { useState, useEffect } from 'react';
import {
  Grid, Card, CardContent, Typography, Box, LinearProgress,
  Alert, IconButton, Tooltip, Table, TableBody, TableCell,
  TableContainer, TableHead, TableRow, Chip,
} from '@mui/material';
import {
  CheckCircle as OkIcon,
  Error as ErrIcon,
  Refresh as RefreshIcon,
  Folder as FolderIcon,
  PlayArrow as RunIcon,
  CheckCircle as PassedIcon,
  Cancel as FailedIcon,
  HourglassEmpty as PendingIcon,
  RadioButtonUnchecked as SkippedIcon,
  Block as CanceledIcon,
} from '@mui/icons-material';
import { getDashboard } from '../api/client';

function StatCard({ title, value, icon, color = 'primary', loading = false }) {
  return (
    <Card>
      <CardContent>
        <Box sx={{ display: 'flex', alignItems: 'center', mb: 1 }}>
          {icon}
          <Typography variant="body2" color="text.secondary" sx={{ ml: 1 }}>
            {title}
          </Typography>
        </Box>
        {loading ? (
          <LinearProgress sx={{ mt: 1 }} />
        ) : (
          <Typography variant="h4" color={color}>
            {value}
          </Typography>
        )}
      </CardContent>
    </Card>
  );
}

function statusIcon(status) {
  switch (status) {
    case 'passed': return <PassedIcon fontSize="small" color="success" />;
    case 'failed': return <FailedIcon fontSize="small" color="error" />;
    case 'running': return <RunIcon fontSize="small" color="warning" />;
    case 'pending': return <PendingIcon fontSize="small" color="disabled" />;
    case 'skipped': return <SkippedIcon fontSize="small" color="disabled" />;
    case 'canceled': return <CanceledIcon fontSize="small" color="disabled" />;
    default: return null;
  }
}

function statusChip(status) {
  const colors = {
    passed: 'success', failed: 'error', running: 'warning',
    pending: 'default', skipped: 'default', canceled: 'default',
  };
  return (
    <Chip
      icon={statusIcon(status)}
      label={status}
      size="small"
      color={colors[status] || 'default'}
      variant="outlined"
    />
  );
}

export default function Dashboard() {
  const [data, setData] = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);

  const fetchDashboard = async () => {
    setLoading(true);
    try {
      const d = await getDashboard();
      setData(d);
      setError(null);
    } catch (e) {
      setError(e.message);
    }
    setLoading(false);
  };

  useEffect(() => {
    fetchDashboard();
    const interval = setInterval(fetchDashboard, 30000);
    return () => clearInterval(interval);
  }, []);

  const stats = data?.run_stats || {};
  const totalRuns = Object.values(stats).reduce((a, b) => a + b, 0);
  const recentRuns = data?.recent_runs || [];

  return (
    <Box>
      <Box sx={{ display: 'flex', alignItems: 'center', mb: 3 }}>
        <Typography variant="h4" sx={{ flexGrow: 1 }}>
          Dashboard
        </Typography>
        <Tooltip title="Refresh">
          <IconButton onClick={fetchDashboard}>
            <RefreshIcon />
          </IconButton>
        </Tooltip>
      </Box>

      {error && (
        <Alert severity="error" sx={{ mb: 2 }}>
          Server unreachable: {error}. Start with <code>make dev</code>.
        </Alert>
      )}

      <Grid container spacing={3}>
        <Grid item xs={6} sm={3} md={2}>
          <StatCard
            title="Projects"
            value={data?.project_count ?? '—'}
            icon={<FolderIcon color="primary" />}
            loading={loading}
          />
        </Grid>
        <Grid item xs={6} sm={3} md={2}>
          <StatCard
            title="Total Runs"
            value={totalRuns}
            icon={<RunIcon color="primary" />}
            loading={loading}
          />
        </Grid>
        <Grid item xs={6} sm={3} md={2}>
          <StatCard
            title="Passed"
            value={stats.passed ?? 0}
            icon={<PassedIcon color="success" />}
            color="success.main"
            loading={loading}
          />
        </Grid>
        <Grid item xs={6} sm={3} md={2}>
          <StatCard
            title="Failed"
            value={stats.failed ?? 0}
            icon={<FailedIcon color="error" />}
            color="error.main"
            loading={loading}
          />
        </Grid>
        <Grid item xs={6} sm={3} md={2}>
          <StatCard
            title="Running"
            value={stats.running ?? 0}
            icon={<RunIcon color="warning" />}
            color="warning.main"
            loading={loading}
          />
        </Grid>
        <Grid item xs={6} sm={3} md={2}>
          <StatCard
            title="Pending"
            value={stats.pending ?? 0}
            icon={<PendingIcon color="disabled" />}
            color="text.secondary"
            loading={loading}
          />
        </Grid>
      </Grid>

      <Card sx={{ mt: 3 }}>
        <CardContent>
          <Typography variant="h6" gutterBottom>
            Recent Runs
          </Typography>
          {!data?.pg_available ? (
            <Alert severity="info">
              Run history requires PostgreSQL. Start with <code>make docker-up</code> and set <code>DATABASE_URL</code>.
            </Alert>
          ) : recentRuns.length === 0 ? (
            <Typography variant="body2" color="text.secondary">
              No runs yet. Trigger a pipeline to see results here.
            </Typography>
          ) : (
            <TableContainer>
              <Table size="small">
                <TableHead>
                  <TableRow>
                    <TableCell>Project</TableCell>
                    <TableCell>Status</TableCell>
                    <TableCell>Started</TableCell>
                  </TableRow>
                </TableHead>
                <TableBody>
                  {recentRuns.map((run) => (
                    <TableRow key={run.id} hover>
                      <TableCell>{run.project_name}</TableCell>
                      <TableCell>{statusChip(run.status)}</TableCell>
                      <TableCell>
                        {run.started_at ? new Date(run.started_at).toLocaleString() : '—'}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </TableContainer>
          )}
        </CardContent>
      </Card>

      <Card sx={{ mt: 3 }}>
        <CardContent>
          <Typography variant="h6" gutterBottom>
            Quick Start
          </Typography>
          <Box component="pre" sx={{
            p: 2, borderRadius: 1, fontFamily: 'monospace',
            backgroundColor: 'background.default', fontSize: 14, overflow: 'auto',
          }}>
{`# Start dev server (hot reload)
make dev

# Or start normally
make serve

# Validate pipeline
make validate

# Resolve triggers
./bin/kestrix resolve --changed auth_service examples/demo-mono/pipeline.tf

# Docker full stack
make docker-up`}
          </Box>
        </CardContent>
      </Card>

      <Card sx={{ mt: 3 }}>
        <CardContent>
          <Typography variant="h6" gutterBottom>
            Architecture
          </Typography>
          <Box component="pre" sx={{
            p: 2, borderRadius: 1, fontFamily: 'monospace',
            backgroundColor: 'background.default', fontSize: 13, overflow: 'auto',
          }}>
{`GitLab Push → Webhook (/webhook)
    ↓
FileClassifier (code vs docs)
    ↓
HCL Parser (pipeline.tf → Project)
    ↓
DAG Engine (resolve triggers)
    ↓
CI Generator (→ gitlab-ci.yml)
    ↓
Dispatch (→ GitLab API)`}
          </Box>
        </CardContent>
      </Card>
    </Box>
  );
}
