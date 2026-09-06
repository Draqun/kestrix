import { useState, useEffect, useCallback, useRef } from 'react';
import {
  Box, Typography, Card, CardContent, Table, TableBody, TableCell,
  TableContainer, TableHead, TableRow, Chip, IconButton, Button,
  Tooltip, Alert, Badge, LinearProgress, TextField,
  FormControl, InputLabel, Select, MenuItem,
} from '@mui/material';
import {
  Refresh as RefreshIcon,
  CheckCircle as OkIcon,
  Info as InfoIcon,
  ErrorOutlined as ErrorIcon,
  RemoveCircleOutlined as IgnoredIcon,
} from '@mui/icons-material';
import Pagination from '../components/Pagination';
import { getWebhookLogs } from '../api/client.js';

const STATUSES = ['triggered', 'skipped', 'ignored', 'error'];

const PROJECT_FILTER_DEBOUNCE_MS = 300;

// STATUS_DISPLAY maps a recorded status to its icon and tooltip. Every status
// the server can record has an entry, so the table never falls back to a
// misleading default.
const STATUS_DISPLAY = {
  triggered: { icon: <OkIcon color="success" />, title: 'Tasks triggered' },
  skipped: { icon: <InfoIcon color="disabled" />, title: 'Skipped — no task matched' },
  ignored: { icon: <IgnoredIcon color="disabled" />, title: 'Ignored — no registered project' },
  error: { icon: <ErrorIcon color="error" />, title: 'Processing failed' },
};

// toRFC3339 converts a datetime-local input value to the RFC3339 form the API
// expects. An empty input stays empty so the filter is simply not applied.
function toRFC3339(value) {
  if (!value) return '';
  const parsed = new Date(value);
  return Number.isNaN(parsed.getTime()) ? '' : parsed.toISOString();
}

export default function WebhookLogs() {
  const [logs, setLogs] = useState([]);
  const [pagination, setPagination] = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [limit, setLimit] = useState(25);
  const [offset, setOffset] = useState(0);
  const [projectInput, setProjectInput] = useState('');
  const [project, setProject] = useState('');
  const [status, setStatus] = useState('');
  const [since, setSince] = useState('');
  const [until, setUntil] = useState('');

  // Requests are not serialized, and the debounce makes overlaps likely: a
  // slow unfiltered page-load response can resolve after a filtered one. Only
  // the newest request is allowed to write state, so the table always matches
  // the filters that were actually applied.
  const requestIdRef = useRef(0);

  const fetchLogs = useCallback(async () => {
    const requestId = ++requestIdRef.current;
    setLoading(true);
    setError('');
    try {
      const data = await getWebhookLogs({
        limit, offset, project, status,
        since: toRFC3339(since), until: toRFC3339(until),
      });
      if (requestId !== requestIdRef.current) return;
      setLogs(data.items);
      setPagination(data.pagination);
    } catch (err) {
      if (requestId !== requestIdRef.current) return;
      setLogs([]);
      setPagination(null);
      setError(err.message || 'Failed to load webhook history');
    }
    setLoading(false);
  }, [limit, offset, project, status, since, until]);

  // The project filter is free text, so it is debounced: without this every
  // keystroke would issue its own GET /api/logs.
  useEffect(() => {
    const timer = setTimeout(() => {
      setProject(projectInput);
      setOffset(0);
    }, PROJECT_FILTER_DEBOUNCE_MS);
    return () => clearTimeout(timer);
  }, [projectInput]);

  useEffect(() => { fetchLogs(); }, [fetchLogs]);

  const formatTime = (ts) => {
    const d = new Date(ts);
    return d.toLocaleTimeString() + ' ' + d.toLocaleDateString();
  };

  const clearFilters = () => {
    setProjectInput('');
    setProject('');
    setStatus('');
    setSince('');
    setUntil('');
    setOffset(0);
  };

  // Derived from the applied project, not the input, so the empty state and
  // the Clear button never describe a filter the last request did not carry.
  const filtersActive = project !== '' || status !== '' || since !== '' || until !== '';
  const total = pagination?.total ?? 0;

  return (
    <Box>
      <Box sx={{ display: 'flex', alignItems: 'center', mb: 3 }}>
        <Typography variant="h4" sx={{ flexGrow: 1 }}>
          Webhook Logs
          <Badge badgeContent={total} color="primary" sx={{ ml: 2 }} />
        </Typography>
        <Tooltip title="Refresh">
          <IconButton onClick={fetchLogs} aria-label="Refresh"><RefreshIcon /></IconButton>
        </Tooltip>
      </Box>

      <Box sx={{ display: 'flex', gap: 2, flexWrap: 'wrap', mb: 2, alignItems: 'center' }}>
        <TextField
          size="small" label="Project" value={projectInput}
          onChange={(e) => setProjectInput(e.target.value)}
          sx={{ minWidth: 180 }}
        />
        <FormControl size="small" sx={{ minWidth: 180 }}>
          <InputLabel id="webhook-status-filter-label">Status</InputLabel>
          <Select
            labelId="webhook-status-filter-label"
            label="Status"
            value={status}
            onChange={(e) => { setStatus(e.target.value); setOffset(0); }}
          >
            <MenuItem value="">All statuses</MenuItem>
            {STATUSES.map((s) => <MenuItem key={s} value={s}>{s}</MenuItem>)}
          </Select>
        </FormControl>
        <TextField
          size="small" type="datetime-local" label="From" value={since}
          onChange={(e) => { setSince(e.target.value); setOffset(0); }}
          InputLabelProps={{ shrink: true }} sx={{ minWidth: 210 }}
        />
        <TextField
          size="small" type="datetime-local" label="To" value={until}
          onChange={(e) => { setUntil(e.target.value); setOffset(0); }}
          InputLabelProps={{ shrink: true }} sx={{ minWidth: 210 }}
        />
        <Button variant="outlined" size="small" onClick={clearFilters} disabled={!filtersActive}>
          Clear filters
        </Button>
      </Box>

      {loading && <LinearProgress sx={{ mb: 2 }} />}

      {error && <Alert severity="error" sx={{ mb: 2 }}>{error}</Alert>}

      {!loading && !error && logs.length === 0 && (
        <Alert severity="info" sx={{ mb: 2 }}>
          {filtersActive
            ? 'No webhook events match these filters.'
            : 'No webhook events recorded yet.'}
        </Alert>
      )}

      <Card>
        <CardContent>
          <TableContainer>
            <Table>
              <TableHead>
                <TableRow>
                  <TableCell width={180}>Timestamp</TableCell>
                  <TableCell>Project</TableCell>
                  <TableCell>Repository</TableCell>
                  <TableCell>Branch</TableCell>
                  <TableCell>Revision</TableCell>
                  <TableCell align="center">Commits</TableCell>
                  <TableCell>Triggered Tasks</TableCell>
                  <TableCell>Pipeline</TableCell>
                  <TableCell align="center">Status</TableCell>
                </TableRow>
              </TableHead>
              <TableBody>
                {logs.map(log => (
                  <TableRow key={log.id} hover>
                    <TableCell>
                      <Typography variant="body2" color="text.secondary">
                        {formatTime(log.received_at)}
                      </Typography>
                    </TableCell>
                    <TableCell>{log.project_name || <Typography variant="body2" color="text.secondary">—</Typography>}</TableCell>
                    <TableCell><strong>{log.repo}</strong></TableCell>
                    <TableCell>
                      {log.branch ? <Chip label={log.branch} size="small" variant="outlined" /> : '—'}
                    </TableCell>
                    <TableCell>
                      <Typography variant="body2" sx={{ fontFamily: 'monospace', fontSize: 13 }}>
                        {log.revision ? log.revision.slice(0, 8) : '—'}
                      </Typography>
                    </TableCell>
                    <TableCell align="center">{log.commits}</TableCell>
                    <TableCell>
                      {log.triggered_tasks?.length > 0 ? (
                        <Box sx={{ display: 'flex', gap: 0.5, flexWrap: 'wrap' }}>
                          {log.triggered_tasks.map(t => (
                            <Chip key={t} label={t} size="small" color="success" variant="outlined" />
                          ))}
                        </Box>
                      ) : (
                        <Typography variant="body2" color="text.secondary">—</Typography>
                      )}
                    </TableCell>
                    <TableCell>
                      {log.dispatched_pipeline_url ? (
                        <a href={log.dispatched_pipeline_url} target="_blank" rel="noreferrer">
                          #{log.dispatched_pipeline_id}
                        </a>
                      ) : (
                        <Typography variant="body2" color="text.secondary">—</Typography>
                      )}
                    </TableCell>
                    <TableCell align="center">
                      <Tooltip title={log.error || STATUS_DISPLAY[log.status]?.title || log.status}>
                        <span>{STATUS_DISPLAY[log.status]?.icon || <InfoIcon color="disabled" />}</span>
                      </Tooltip>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </TableContainer>
        </CardContent>
      </Card>

      <Pagination
        pagination={pagination}
        onPageChange={setOffset}
        onLimitChange={(newLimit) => { setLimit(newLimit); setOffset(0); }}
      />
    </Box>
  );
}
