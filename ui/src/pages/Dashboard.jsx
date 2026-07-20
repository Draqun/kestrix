import { useState, useEffect } from 'react';
import {
  Grid, Card, CardContent, Typography, Box, LinearProgress,
  Alert, IconButton, Tooltip
} from '@mui/material';
import {
  CheckCircle as OkIcon,
  Error as ErrIcon,
  Refresh as RefreshIcon,
  Webhook as WebhookIcon,
  AccountTree as DagIcon,
  Folder as FolderIcon,
} from '@mui/icons-material';
import { healthCheck } from '../api/client';

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

export default function Dashboard() {
  const [health, setHealth] = useState(null);
  const [loading, setLoading] = useState(true);

  const fetchHealth = async () => {
    setLoading(true);
    try {
      const data = await healthCheck();
      setHealth(data);
    } catch (e) {
      setHealth({ status: 'error', error: e.message });
    }
    setLoading(false);
  };

  useEffect(() => {
    fetchHealth();
    const interval = setInterval(fetchHealth, 30000);
    return () => clearInterval(interval);
  }, []);

  return (
    <Box>
      <Box sx={{ display: 'flex', alignItems: 'center', mb: 3 }}>
        <Typography variant="h4" sx={{ flexGrow: 1 }}>
          Dashboard
        </Typography>
        <Tooltip title="Refresh">
          <IconButton onClick={fetchHealth}>
            <RefreshIcon />
          </IconButton>
        </Tooltip>
      </Box>

      {health?.status === 'error' && (
        <Alert severity="error" sx={{ mb: 2 }}>
          Server unreachable: {health.error}. Start with <code>make serve</code> or <code>make dev</code>.
        </Alert>
      )}

      <Grid container spacing={3}>
        <Grid item xs={12} sm={6} md={3}>
          <StatCard
            title="Server Status"
            value={health?.status === 'ok' ? 'Online' : 'Offline'}
            icon={health?.status === 'ok'
              ? <OkIcon color="success" />
              : <ErrIcon color="error" />}
            color={health?.status === 'ok' ? 'success.main' : 'error.main'}
            loading={loading}
          />
        </Grid>
        <Grid item xs={12} sm={6} md={3}>
          <StatCard
            title="Pipelines"
            value={health?.pipelines ?? '—'}
            icon={<DagIcon color="primary" />}
            loading={loading}
          />
        </Grid>
        <Grid item xs={12} sm={6} md={3}>
          <StatCard
            title="Webhooks Today"
            value={health?.webhooks ?? '—'}
            icon={<WebhookIcon color="secondary" />}
            loading={loading}
          />
        </Grid>
        <Grid item xs={12} sm={6} md={3}>
          <StatCard
            title="Repos Tracked"
            value={health?.repos ?? '—'}
            icon={<FolderIcon color="warning" />}
            loading={loading}
          />
        </Grid>
      </Grid>

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
./bin/metaci resolve --changed auth_service examples/demo-mono/pipeline.tf

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
