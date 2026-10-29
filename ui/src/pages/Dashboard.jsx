import { useState, useEffect } from 'react';
import { Link as RouterLink, useNavigate } from 'react-router-dom';
import {
  Button, Skeleton, Card, CardContent, Typography, Box,
  Alert, IconButton, Tooltip, Table, TableBody, TableCell,
  TableContainer, TableHead, TableRow, Chip,
} from '@mui/material';
import {
  Refresh as RefreshIcon,
  PlayArrow as RunIcon,
  CheckCircle as PassedIcon,
  Cancel as FailedIcon,
  HourglassEmpty as PendingIcon,
  RadioButtonUnchecked as SkippedIcon,
  Block as CanceledIcon,
} from '@mui/icons-material';
import { getDashboard } from '../api/client';

function StatCard({ title, value, color = 'text.primary', loading = false }) {
  return (
    <Box sx={{ p: 2, minWidth: 0 }}>
      <Typography variant="body2" color="text.secondary" sx={{ mb: 1 }}>{title}</Typography>
      {loading ? <Skeleton variant="text" width={48} height={34} animation={false} /> : (
        <Typography sx={{ fontSize: 28, lineHeight: 1.2, fontWeight: 400, fontVariantNumeric: 'tabular-nums', color }}>
          {value}
        </Typography>
      )}
    </Box>
  );
}

function statusIcon(status) {
  switch (status) {
    case 'passed': return <PassedIcon fontSize="small" color="success" />;
    case 'failed': return <FailedIcon fontSize="small" color="error" />;
    case 'running': return <RunIcon fontSize="small" color="warning" />;
    // 'queued' (waiting for a concurrency slot) intentionally reuses
    // PendingIcon from 'pending' (waiting on task dependencies) - the two
    // statuses are conceptually different, but share an hourglass glyph,
    // distinguished only by color (info vs disabled).
    case 'queued': return <PendingIcon fontSize="small" color="info" />;
    case 'pending': return <PendingIcon fontSize="small" color="disabled" />;
    case 'skipped': return <SkippedIcon fontSize="small" color="disabled" />;
    case 'canceled': return <CanceledIcon fontSize="small" color="disabled" />;
    default: return null;
  }
}

function statusChip(status) {
  const colors = {
    passed: 'success', failed: 'error', running: 'warning', queued: 'info',
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
  const navigate = useNavigate();
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
        <Typography component="h1" variant="h4" sx={{ flexGrow: 1 }}>
          Dashboard
        </Typography>
        <Button component={RouterLink} to="/pipelines" variant="contained" sx={{ mr: 1 }}>View pipelines</Button>
        <Tooltip title="Refresh">
          <IconButton aria-label="Refresh dashboard" onClick={fetchDashboard}>
            <RefreshIcon />
          </IconButton>
        </Tooltip>
      </Box>

      {error && (
        <Alert severity="error" sx={{ mb: 2 }}>
          Server unreachable: {error}. Start with <code>make dev</code>.
        </Alert>
      )}

      {/* Four equal columns; 2 on phones. Secondary states share a compact line below. */}
      <Box sx={{ border: '1px solid', borderColor: 'divider', borderRadius: 1, overflow: 'hidden' }}>
        <Box sx={{ display: 'grid', gridTemplateColumns: { xs: 'repeat(2, minmax(0, 1fr))', md: 'repeat(4, minmax(0, 1fr))' } }}>
          <StatCard title="Projects" value={data?.project_count ?? '—'} loading={loading && !data} />
          <StatCard title="Total Runs" value={totalRuns} loading={loading && !data} />
          <StatCard title="Passed" value={stats.passed ?? 0} color={stats.passed ? 'success.main' : 'text.primary'} loading={loading && !data} />
          <StatCard title="Failed" value={stats.failed ?? 0} color={stats.failed ? 'error.main' : 'text.primary'} loading={loading && !data} />
        </Box>
        <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 2.5, px: 2, py: 1.5, bgcolor: 'action.hover' }}>
          {['running', 'queued', 'pending', 'canceled', 'skipped'].map(status => (
            <Box key={status} sx={{ display: 'flex', alignItems: 'center', gap: 0.75 }}>
              <Typography variant="body2" color="text.secondary" sx={{ textTransform: 'capitalize' }}>{status[0].toUpperCase() + status.slice(1)}</Typography>
              {loading ? <Skeleton width={16} animation={false} /> : <Typography variant="body2" sx={{ fontWeight: 600 }}>{stats[status] ?? 0}</Typography>}
            </Box>
          ))}
        </Box>
      </Box>

      <Card sx={{ mt: 3 }}>
        <CardContent>
          <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', mb: 1 }}>
            <Typography component="h2" variant="h6">Recent Runs</Typography>
            <Button component={RouterLink} to="/pipelines" size="small">View all</Button>
          </Box>
          {loading && !data ? (
            <Skeleton variant="rectangular" height={96} animation={false} />
          ) : !data?.pg_available ? (
            <Alert severity="info">
              Run history requires PostgreSQL. Start with <code>make docker-up</code> and set <code>DATABASE_URL</code>.
            </Alert>
          ) : recentRuns.length === 0 ? (
            <Typography variant="body2" color="text.secondary">
              No runs yet. Trigger a pipeline to see results here.
            </Typography>
          ) : (
            <TableContainer>
              <Table size="small" sx={{
                '@media (max-width: 599px)': {
                  display: 'block',
                  '& thead': { display: 'none' },
                  '& tbody': { display: 'block' },
                  '& tr': { display: 'grid', gridTemplateColumns: 'minmax(0, 1fr) auto', py: 1.5, borderBottom: '1px solid', borderColor: 'divider' },
                  '& td': { display: 'block', border: 0, px: 0, py: 0.25 },
                  '& td:first-of-type': { fontWeight: 500, overflowWrap: 'anywhere' },
                  '& td:nth-of-type(n+3)': { gridColumn: '1 / -1', color: 'text.secondary', fontSize: 12 },
                },
              }}>
                <TableHead>
                  <TableRow>
                    <TableCell>Project</TableCell>
                    <TableCell>Status</TableCell>
                    <TableCell>Started</TableCell>
                    <TableCell>Finished</TableCell>
                  </TableRow>
                </TableHead>
                <TableBody>
                  {recentRuns.slice(0, 5).map((run) => (
                    <TableRow
                      key={run.id}
                      hover
                      tabIndex={0}
                      onKeyDown={(event) => {
                        if (event.key === 'Enter') navigate(`/pipelines/runs/${encodeURIComponent(run.project_name)}/${run.id}`);
                      }}
                      onClick={() => navigate(`/pipelines/runs/${encodeURIComponent(run.project_name)}/${run.id}`)}
                      sx={{ cursor: 'pointer' }}
                    >
                      <TableCell>{run.project_name}</TableCell>
                      <TableCell>{statusChip(run.status)}</TableCell>
                      <TableCell>
                        <Box component="span" sx={{ display: { xs: 'inline', sm: 'none' } }}>Started: </Box>
                        {run.started_at ? new Date(run.started_at).toLocaleString() : '—'}
                      </TableCell>
                      <TableCell>
                        <Box component="span" sx={{ display: { xs: 'inline', sm: 'none' } }}>Finished: </Box>
                        {run.finished_at ? new Date(run.finished_at).toLocaleString() : '—'}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </TableContainer>
          )}
        </CardContent>
      </Card>

      <Box sx={{ display: 'grid', gridTemplateColumns: { xs: '1fr', lg: 'minmax(0, 3fr) minmax(0, 2fr)' }, gap: 3, mt: 3 }}>
      <Card>
        <CardContent>
          <Typography component="h2" variant="h6" gutterBottom>
            Quick Start
          </Typography>
          <Box component="pre" sx={{
            p: 2, borderRadius: 1, fontFamily: '"IBM Plex Mono", monospace',
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

      <Card>
        <CardContent>
          <Typography component="h2" variant="h6" gutterBottom>
            Architecture
          </Typography>
          <Box component="pre" sx={{
            p: 2, borderRadius: 1, fontFamily: '"IBM Plex Mono", monospace',
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
    </Box>
  );
}
