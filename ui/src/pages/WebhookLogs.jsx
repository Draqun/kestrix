import { useState, useEffect } from 'react';
import {
  Box, Typography, Card, CardContent, Table, TableBody, TableCell,
  TableContainer, TableHead, TableRow, Paper, Chip, IconButton,
  Tooltip, Alert, Badge, LinearProgress
} from '@mui/material';
import {
  Refresh as RefreshIcon,
  CheckCircle as OkIcon,
  Error as ErrIcon,
  Info as InfoIcon,
} from '@mui/icons-material';

// Mock webhook logs for PoC (will be replaced with real API)
const MOCK_LOGS = [
  {
    id: 1, timestamp: new Date(Date.now() - 300000).toISOString(),
    repo: 'auth_service', branch: 'main', status: 'triggered',
    tasks: ['test_auth', 'integration_tests'], commits: 2,
    message: 'fix: handler validation',
  },
  {
    id: 2, timestamp: new Date(Date.now() - 600000).toISOString(),
    repo: 'billing_service', branch: 'main', status: 'triggered',
    tasks: ['test_billing'], commits: 1,
    message: 'feat: add payment retry',
  },
  {
    id: 3, timestamp: new Date(Date.now() - 900000).toISOString(),
    repo: 'docs', branch: 'main', status: 'skipped',
    tasks: [], commits: 3,
    message: 'docs: update API reference',
  },
  {
    id: 4, timestamp: new Date(Date.now() - 1200000).toISOString(),
    repo: 'api_gateway', branch: 'develop', status: 'triggered',
    tasks: ['test_auth', 'test_billing', 'integration_tests'], commits: 5,
    message: 'chore: update dependencies',
  },
];

export default function WebhookLogs() {
  const [logs, setLogs] = useState([]);
  const [loading, setLoading] = useState(true);

  const fetchLogs = async () => {
    setLoading(true);
    try {
      const res = await fetch('http://localhost:8080/api/logs');
      if (res.ok) {
        const data = await res.json();
        setLogs(data.length > 0 ? data : MOCK_LOGS);
      } else {
        setLogs(MOCK_LOGS);
      }
    } catch {
      setLogs(MOCK_LOGS);
    }
    setLoading(false);
  };

  useEffect(() => { fetchLogs(); }, []);

  const formatTime = (ts) => {
    const d = new Date(ts);
    return d.toLocaleTimeString() + ' ' + d.toLocaleDateString();
  };

  return (
    <Box>
      <Box sx={{ display: 'flex', alignItems: 'center', mb: 3 }}>
        <Typography variant="h4" sx={{ flexGrow: 1 }}>
          Webhook Logs
          <Badge badgeContent={logs.length} color="primary" sx={{ ml: 2 }} />
        </Typography>
        <Tooltip title="Refresh">
          <IconButton onClick={fetchLogs}><RefreshIcon /></IconButton>
        </Tooltip>
      </Box>

      {loading && <LinearProgress sx={{ mb: 2 }} />}

      <Alert severity="info" sx={{ mb: 2 }}>
        {logs === MOCK_LOGS ? 'Showing mock data — start server with `make serve` for live logs' : 'Live from server'}
      </Alert>

      <Card>
        <CardContent>
          <TableContainer>
            <Table>
              <TableHead>
                <TableRow>
                  <TableCell width={180}>Timestamp</TableCell>
                  <TableCell>Repository</TableCell>
                  <TableCell>Branch</TableCell>
                  <TableCell>Message</TableCell>
                  <TableCell>Commits</TableCell>
                  <TableCell>Triggered Tasks</TableCell>
                  <TableCell align="center">Status</TableCell>
                </TableRow>
              </TableHead>
              <TableBody>
                {logs.map(log => (
                  <TableRow key={log.id} hover>
                    <TableCell>
                      <Typography variant="body2" color="text.secondary">
                        {formatTime(log.timestamp)}
                      </Typography>
                    </TableCell>
                    <TableCell>
                      <strong>{log.repo}</strong>
                    </TableCell>
                    <TableCell>
                      <Chip label={log.branch} size="small" variant="outlined" />
                    </TableCell>
                    <TableCell>
                      <Typography variant="body2" sx={{ fontFamily: 'monospace', fontSize: 13 }}>
                        {log.message}
                      </Typography>
                    </TableCell>
                    <TableCell align="center">{log.commits}</TableCell>
                    <TableCell>
                      {log.tasks.length > 0 ? (
                        <Box sx={{ display: 'flex', gap: 0.5, flexWrap: 'wrap' }}>
                          {log.tasks.map(t => (
                            <Chip key={t} label={t} size="small" color="success" variant="outlined" />
                          ))}
                        </Box>
                      ) : (
                        <Typography variant="body2" color="text.secondary">—</Typography>
                      )}
                    </TableCell>
                    <TableCell align="center">
                      {log.status === 'triggered' ? (
                        <Tooltip title="Tasks triggered">
                          <OkIcon color="success" />
                        </Tooltip>
                      ) : (
                        <Tooltip title="Skipped (no code changes)">
                          <InfoIcon color="disabled" />
                        </Tooltip>
                      )}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </TableContainer>
        </CardContent>
      </Card>
    </Box>
  );
}
