import { useState, useEffect, useCallback, useMemo, useRef } from 'react';
import { useSearchParams } from 'react-router-dom';
import {
  useNodesState, useEdgesState
} from 'reactflow';
import 'reactflow/dist/style.css';
import {
  Box, Card, CardContent, Typography, Button, TextField, Chip,
  Alert, LinearProgress, FormControlLabel, Switch,
  Dialog, DialogTitle, DialogContent, DialogActions, IconButton, Divider,
  List, ListItem, ListItemText, ListItemIcon, ListItemButton,
  Select, MenuItem, FormControl, InputLabel,
} from '@mui/material';
import {
  PlayArrow as ResolveIcon,
  RocketLaunch as RunIcon,
  CheckCircle as TriggeredIcon,
  AccountTree as DagIcon,
  ArrowBack as BackIcon,
  Close as CloseIcon,
  History as HistoryIcon,
  Visibility as ViewIcon,
  Add as AddIcon,
  Replay as RerunIcon,
} from '@mui/icons-material';
import CodeMirror from '@uiw/react-codemirror';
import { oneDark } from '@codemirror/theme-one-dark';
import DAGViewer from '../components/DAGViewer';
import CopyButton from '../components/CopyButton';
import { layoutFromProject } from '../lib/dagLayout';
import { getProjectPipeline, getProjectDefinition, resolveProject, runProject, listProjects, listAllRuns, getRun, cancelRun, approveManualTask, runTask } from '../api/client';
import { filterRuns, getUniqueRepos, RUN_STATUSES } from '../lib/runFilters.js';
import Pagination from '../components/Pagination';
import { useAuth } from '../context/AuthContext';




function formatStatus(status) {
  if (status === 'passed') return { color: 'success', label: 'PASSED' };
  if (status === 'failed') return { color: 'error', label: 'FAILED' };
  if (status === 'canceled') return { color: 'warning', label: 'CANCELED' };
  if (status === 'queued') return { color: 'info', label: 'QUEUED' };
  if (status === 'manual') return { color: 'info', label: 'MANUAL' };
  if (status === 'skipped') return { color: 'default', label: 'SKIPPED' };
  if (status === 'pending') return { color: 'default', label: 'PENDING' };
  return { color: 'warning', label: 'RUNNING' };
}

// actorLabel renders an attribution actor string for display: an empty or
// missing actor (a run recorded before run-attribution existed) shows as
// "unknown", and the "user:" prefix is stripped so the account name reads
// on its own. See run-attribution: "Attribution is shown in the interface".
function actorLabel(actor) {
  if (!actor) return 'unknown';
  return actor.startsWith('user:') ? actor.slice('user:'.length) : actor;
}

function isManualWaiting(job, pipelineDef, allJobs) {
  if (!job) return false;
  const waiting = job.status === 'manual' ||
    (job.status === 'skipped' && job.error !== 'dependency failed' && job.error !== 'dependency canceled');
  if (!waiting) return false;
  // Must be manual in pipeline definition.
  const t = pipelineDef?.tasks?.find(x => (x.name || x.Name) === job.name);
  if (!(t?.manual ?? t?.Manual)) return false;
  // All dependencies must have passed.
  const deps = t.depends_on || t.DependsOn || [];
  if (deps.length === 0) return true;
  const statusMap = {};
  (allJobs || []).forEach(j => { statusMap[j.name] = j.status; });
  return deps.every(d => statusMap[d] === 'passed');
}

const TERMINAL_RUN_STATUSES = ['passed', 'failed', 'canceled'];

// canRerun reports whether a task of a run may be re-run on its own: the run
// and the task must both have finished (passed, failed, or canceled). A task
// waiting for manual approval is `manual` or `skipped`, never one of those,
// so it keeps its approval action and never gets this one; a task skipped
// because a dependency failed or was canceled never ran and is not offered
// either - see ui-single-task-rerun design.md D5.
function canRerun(job, run) {
  return TERMINAL_RUN_STATUSES.includes(run?.status) && TERMINAL_RUN_STATUSES.includes(job?.status);
}

// RerunButton is the Re-run action for one task, shown in the job list and
// the selected-job header. rerunning names the task whose re-run is in
// flight, if any; every Re-run button is disabled while one is.
function RerunButton({ task, rerunning, onRerun, sx }) {
  return (
    <Button size="small" variant="outlined" startIcon={<RerunIcon />} sx={sx}
      disabled={!!rerunning} onClick={() => onRerun(task)}>
      {rerunning === task ? 'Starting…' : 'Re-run'}
    </Button>
  );
}

function formatDuration(ms) {
  if (!ms) return '';
  if (ms < 1000) return `${ms}ms`;
  return `${(ms / 1000).toFixed(1)}s`;
}

export function RunDetail({ projectName, runId, onBack, onRerunStarted }) {
  const { roleAtLeast } = useAuth();
  const canOperate = roleAtLeast('operator');
  const [detail, setDetail] = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);
  const [selectedJob, setSelectedJob] = useState(null);
  const [dagNodes, setDagNodes, onDagNodesChange] = useNodesState([]);
  const [dagEdges, setDagEdges, onDagEdgesChange] = useEdgesState([]);
  const [canceling, setCanceling] = useState(false);
  const [approving, setApproving] = useState(null);
  const [approveErr, setApproveErr] = useState(null);
  const [rerunning, setRerunning] = useState(null);
  const [rerunErr, setRerunErr] = useState(null);
  // The run this view is routed to - set as soon as runId changes, before
  // that run has loaded - or null once it unmounts. A load or re-run
  // response for any other run arrived after the user moved on and must
  // not act on the view.
  const routedRunIdRef = useRef(runId);
  useEffect(() => {
    routedRunIdRef.current = runId;
    return () => { routedRunIdRef.current = null; };
  }, [runId]);

  // A load for a run the view has since left - a poll, or the reload after
  // cancel/approve, still in flight - is discarded rather than applied.
  const load = useCallback(async () => {
    try {
      const d = await getRun(projectName, runId);
      if (routedRunIdRef.current !== runId) return null;
      setDetail(d);
      setError(null);
      return d;
    } catch (e) {
      if (routedRunIdRef.current === runId) setError(e.message);
      return null;
    }
  }, [projectName, runId]);

  useEffect(() => {
    setDetail(null);
    setLoading(true);
    setSelectedJob(null);
    setRerunning(null);
    setRerunErr(null);
    load().finally(() => {
      if (routedRunIdRef.current === runId) setLoading(false);
    });
  }, [load, runId]);

  // Poll while running or a job is running after manual approve.
  useEffect(() => {
    if (!detail) return;
    const jobs = detail.task_results || [];
    const busy = detail.status === 'running' || detail.status === 'queued' || jobs.some(j => j.status === 'running');
    if (!busy) return;
    const id = setInterval(() => { load(); }, 1500);
    return () => clearInterval(id);
  }, [detail?.status, detail?.task_results, load]);

  useEffect(() => {
    if (!detail?.pipeline_def) { setDagNodes([]); setDagEdges([]); return; }
    const proj = {
      Repos: (detail.pipeline_def.repos || []).map(repo => ({
        Name: repo.name ?? repo.Name,
        Path: repo.path ?? repo.Path,
        Kind: repo.kind ?? repo.Kind,
      })),
      Tasks: (detail.pipeline_def.tasks || []).map(t => {
        const ex = t.execute || t.Execute || {};
        return {
          Name: t.name ?? t.Name,
          Trigger: t.trigger ?? t.Trigger,
          DependsOn: t.depends_on ?? t.DependsOn,
          Manual: t.manual ?? t.Manual,
          Execute: {
            Repo: ex.repo ?? ex.Repo,
            Script: ex.script ?? ex.Script,
            Image: ex.image ?? ex.Image,
          },
        };
      }),
    };
    const { nodes: n, edges: e } = layoutFromProject(proj);
    const statusMap = {};
    (detail.task_results || []).forEach(t => { statusMap[t.name] = t.status; });
    setDagNodes(n.map(node => {
      if (node.data.type === 'task') {
        const taskName = node.id.replace('task:', '');
        if (statusMap[taskName]) {
          return { ...node, data: { ...node.data, runStatus: statusMap[taskName] } };
        }
      }
      return node;
    }));
    setDagEdges(e);
  }, [detail?.pipeline_def, detail?.task_results]);

  const handleCancel = async () => {
    setCanceling(true);
    try {
      await cancelRun(projectName, runId);
      await load();
    } catch (e) { setError(e.message); }
    setCanceling(false);
  };

  const handleApprove = async (taskName) => {
    setApproving(taskName);
    setApproveErr(null);
    try {
      await approveManualTask(projectName, runId, taskName);
      await load();
    } catch (e) {
      setApproveErr(e.message);
    }
    setApproving(null);
  };

  // A re-run is a new run: follow it rather than staying on this one, which
  // it leaves unchanged. On rejection, stay here and show why. Either way,
  // do nothing if the user has left this run before the response arrives.
  const handleRerun = async (taskName) => {
    const startedOn = runId;
    setRerunning(taskName);
    setRerunErr(null);
    try {
      const result = await runTask(projectName, taskName);
      if (routedRunIdRef.current !== startedOn) return;
      if (!result.run_id) {
        setRerunErr('Re-run started but no run_id returned');
      } else {
        onRerunStarted?.(result.run_id);
      }
    } catch (e) {
      if (routedRunIdRef.current === startedOn) setRerunErr(e.message);
    } finally {
      // Leaving the run already reset rerunning; clearing it here would
      // cut short a re-run started since on the run the user moved to.
      if (routedRunIdRef.current === startedOn) setRerunning(null);
    }
  };

  // Until the new run loads, detail may still hold the previous run: never
  // render its jobs, or their Re-run buttons, under this run's id.
  const r = detail?.id === runId ? detail : null;
  const st = r ? formatStatus(r.status) : null;
  const jobs = (() => {
    const map = {};
    (r?.task_results || []).forEach(j => { map[j.name] = j; });
    return Object.values(map);
  })();
  const activeJob = selectedJob != null ? jobs.find(j => j.name === selectedJob) : null;

  return (
    <Box sx={{ height: 'calc(100vh - 100px)', display: 'flex', flexDirection: 'column' }}>
      <Box sx={{ display: 'flex', alignItems: 'center', mb: 2, gap: 1, flexWrap: 'wrap' }}>
        <Button size="small" startIcon={<BackIcon />} onClick={onBack}>Back to runs</Button>
        <Typography variant="h4">Run #{runId}</Typography>
        <Typography variant="h6" color="text.secondary">{projectName}</Typography>
        {st && <Chip label={st.label} color={st.color} size="small" />}
        {r?.rerun_task ? (
          <Chip label={`Re-run: ${r.rerun_task}`} size="small" variant="outlined" />
        ) : r?.changed?.length > 0 && (
          <Chip label={`Changed: ${r.changed.join(', ')}`} size="small" variant="outlined" />
        )}
        <Box sx={{ flexGrow: 1 }} />
        {canOperate && (r?.status === 'running' || r?.status === 'queued') && (
          <Button variant="outlined" color="warning" size="small" onClick={handleCancel} disabled={canceling}>
            {canceling ? 'Canceling…' : 'Cancel'}
          </Button>
        )}
      </Box>

      {loading && !r && <LinearProgress sx={{ mb: 2 }} />}
      {error && <Alert severity="error" sx={{ mb: 1 }}>{error}</Alert>}
      {approveErr && <Alert severity="error" sx={{ mb: 1 }} onClose={() => setApproveErr(null)}>{approveErr}</Alert>}
      {rerunErr && <Alert severity="error" sx={{ mb: 1 }} onClose={() => setRerunErr(null)}>{rerunErr}</Alert>}

      {r && (
        <Typography variant="caption" color="text.secondary" display="block" sx={{ mb: 1 }}>
          {new Date(r.started_at).toLocaleString()} by {actorLabel(r.started_by)}
          {r.finished_at && ` → ${new Date(r.finished_at).toLocaleString()}`}
          {r.status === 'canceled' && ` · canceled by ${actorLabel(r.canceled_by)}`}
        </Typography>
      )}

	  {r?.sources?.length > 0 && (
		<Box sx={{ display: 'flex', gap: 1, flexWrap: 'wrap', mb: 1 }}>
		  {r.sources.map((source, index) => (
			<Chip
			  key={`${source.repo_name}-${source.resolved_revision}-${index}`}
			  size="small"
			  variant="outlined"
			  label={`${source.repo_name}${source.primary ? ' (primary)' : ''}: ${source.url} @ ${source.requested_ref} → ${source.resolved_revision}`}
			/>
		  ))}
		</Box>
	  )}

      <Box sx={{ display: 'flex', gap: 2, flex: 1, minHeight: 0 }}>
        <Card sx={{ width: 280, flexShrink: 0, display: 'flex', flexDirection: 'column' }}>
          <CardContent sx={{ flex: 1, overflow: 'auto', py: 1, '&:last-child': { pb: 1 } }}>
            <Typography variant="subtitle2" gutterBottom>Jobs</Typography>
            {jobs.length > 0 ? (
              <List dense disablePadding>
                {jobs.map(job => {
                  const js = formatStatus(job.status);
                  const canRun = isManualWaiting(job, r?.pipeline_def, jobs);
                  return (
                    <ListItem key={job.name} disablePadding sx={{ mb: 0.5, flexDirection: 'column', alignItems: 'stretch' }}>
                      <ListItemButton
                        selected={selectedJob === job.name}
                        onClick={() => setSelectedJob(selectedJob === job.name ? null : job.name)}
                        sx={{ borderRadius: 1, px: 1 }}>
                        <Chip label={js.label} color={js.color} size="small" sx={{ mr: 1, minWidth: 54 }} />
                        <ListItemText
                          primary={job.name}
                          secondary={formatDuration(job.duration)}
                          slotProps={{
                            primary: { variant: 'body2', fontWeight: 600 },
                            secondary: { variant: 'caption' },
                          }}
                        />
                      </ListItemButton>
                      {canOperate && canRun && (
                        <Button size="small" variant="contained" color="info" sx={{ mt: 0.5, mx: 1 }}
                          disabled={!!approving} onClick={() => handleApprove(job.name)}>
                          {approving === job.name ? 'Starting…' : 'Run'}
                        </Button>
                      )}
                      {canOperate && canRerun(job, r) && (
                        <RerunButton task={job.name} rerunning={rerunning} onRerun={handleRerun} sx={{ mt: 0.5, mx: 1 }} />
                      )}
                    </ListItem>
                  );
                })}
              </List>
            ) : (
              <Typography variant="body2" color="text.secondary">
                {loading ? 'Loading…' : 'No job data yet.'}
              </Typography>
            )}
          </CardContent>
        </Card>

        <Card sx={{ flex: 1, display: 'flex', flexDirection: 'column', minWidth: 0 }}>
          <CardContent sx={{ flex: 1, display: 'flex', flexDirection: 'column', p: 1, minHeight: 0, '&:last-child': { pb: 1 } }}>
            {activeJob ? (
              <>
                <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 1, px: 1 }}>
                  <Button size="small" onClick={() => setSelectedJob(null)}>← DAG</Button>
                  <Typography variant="subtitle1" fontWeight={700}>{activeJob.name}</Typography>
                  <Chip label={formatStatus(activeJob.status).label} color={formatStatus(activeJob.status).color} size="small" />
                  {activeJob.duration > 0 && (
                    <Typography variant="caption" color="text.secondary">{formatDuration(activeJob.duration)}</Typography>
                  )}
                  {canOperate && isManualWaiting(activeJob, r?.pipeline_def, jobs) && (
                    <Button size="small" variant="contained" color="info" disabled={!!approving}
                      onClick={() => handleApprove(activeJob.name)}>
                      {approving === activeJob.name ? 'Starting…' : 'Run manual job'}
                    </Button>
                  )}
                  {canOperate && canRerun(activeJob, r) && (
                    <RerunButton task={activeJob.name} rerunning={rerunning} onRerun={handleRerun} />
                  )}
                </Box>
                {activeJob.approved_by && (
                  <Typography variant="caption" color="text.secondary" display="block" sx={{ mb: 1, px: 1 }}>
                    approved by {actorLabel(activeJob.approved_by)}
                    {activeJob.approved_at && ` at ${new Date(activeJob.approved_at).toLocaleString()}`}
                  </Typography>
                )}
                {activeJob.error && activeJob.status !== 'manual' && (
                  <Alert severity="error" sx={{ mb: 1, mx: 1 }}>{activeJob.error}</Alert>
                )}
                {isManualWaiting(activeJob, r?.pipeline_def, jobs) && (
                  <Alert severity="info" sx={{ mb: 1, mx: 1 }}>Waiting for manual approval.</Alert>
                )}
                <Box sx={{ position: 'relative', flex: 1, display: 'flex', flexDirection: 'column', minHeight: 0 }}>
                  <CopyButton text={activeJob.output || ''} />
                  <Box component="pre" sx={{ m: 0, p: 1.5, flex: 1, overflow: 'auto', bgcolor: (theme) => theme.palette.mode === 'dark' ? 'grey.900' : 'grey.200', borderRadius: 1, fontSize: 12, whiteSpace: 'pre-wrap' }}>
                    {activeJob.output || (activeJob.status === 'running' ? 'Running…' : '(No output)')}
                  </Box>
                </Box>
            </>
            ) : (
              <Box sx={{ flex: 1, borderRadius: 1, overflow: 'hidden', minHeight: 320 }}>
                {dagNodes.length > 0 ? (
                  <DAGViewer nodes={dagNodes} edges={dagEdges}
                    onNodeClick={(_, node) => {
                      if (node.data?.type === 'task') setSelectedJob(node.id.replace('task:', ''));
                    }} />
            ) : (
                  <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100%' }}>
                    <Typography variant="body2" color="text.secondary">
                      {loading ? 'Loading pipeline…' : 'No pipeline definition'}
                    </Typography>
                  </Box>
                )}
              </Box>
            )}
            {r?.output && !activeJob && (
              <Box sx={{ position: 'relative', mt: 1 }}>
                <CopyButton text={r.output} />
                <Box component="pre" sx={{ m: 0, p: 1.5, maxHeight: 160, overflow: 'auto', bgcolor: (theme) => theme.palette.mode === 'dark' ? 'grey.900' : 'grey.200', borderRadius: 1, fontSize: 11, whiteSpace: 'pre-wrap' }}>
                  {r.output}
                </Box>
              </Box>
            )}
          </CardContent>
        </Card>
      </Box>
    </Box>
  );
}

export function RunList({ onNewRun, onOpenRun }) {
  const { roleAtLeast } = useAuth();
  const canOperate = roleAtLeast('operator');
  const [searchParams, setSearchParams] = useSearchParams();
  const allowedLimits = [10, 25, 50, 100];
  const limit = allowedLimits.includes(Number(searchParams.get('limit')))
    ? Number(searchParams.get('limit'))
    : 25;
  const page = Math.max(1, Number(searchParams.get('page')) || 1);
  const offset = (page - 1) * limit;

  const [allRuns, setAllRuns] = useState([]);
  const [loading, setLoading] = useState(true);
  const [statusFilter, setStatusFilter] = useState([]);
  const [changeFilter, setChangeFilter] = useState('');

  const loadRuns = useCallback(async () => {
    setLoading(true);
    try {
      const data = await listAllRuns();
      setAllRuns(Array.isArray(data) ? data : []);
    } catch {
      setAllRuns([]);
    }
    setLoading(false);
  }, []);

  useEffect(() => { loadRuns(); }, [loadRuns]);

  const repos = useMemo(() => getUniqueRepos(allRuns), [allRuns]);

  const filteredRuns = useMemo(
    () => filterRuns(allRuns, { statusFilter, changeFilter }),
    [allRuns, statusFilter, changeFilter]
  );

  const total = filteredRuns.length;
  const start = offset;
  const end = Math.min(offset + limit, total);
  const runs = filteredRuns.slice(start, end);

  const pagination = useMemo(() => {
    const nextUrl = end < total ? `/api/runs?limit=${limit}&offset=${end}` : null;
    const prevUrl = offset > 0 ? `/api/runs?limit=${limit}&offset=${Math.max(0, offset - limit)}` : null;
    return { total, limit, offset, next: nextUrl, prev: prevUrl };
  }, [total, limit, offset, end]);

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
    if (page !== 1) {
      setSearchParams(prev => {
        const next = new URLSearchParams(prev);
        next.set('page', '1');
        next.set('limit', String(limit));
        return next;
      });
    }
  }, [statusFilter, changeFilter, page, limit, setSearchParams]);

  const clearFilters = () => {
    setStatusFilter([]);
    setChangeFilter('');
  };

  const handleStatusChange = (event) => {
    const { value } = event.target;
    setStatusFilter(typeof value === 'string' ? value.split(',') : value);
  };

  return (
    <Box>
      <Box sx={{ display: 'flex', alignItems: 'center', mb: 3 }}>
        <Typography variant="h4" sx={{ flexGrow: 1 }}>Runs</Typography>
        <IconButton onClick={loadRuns} title="Refresh">
          <HistoryIcon />
        </IconButton>
        {canOperate && (
          <Button variant="contained" startIcon={<AddIcon />} onClick={onNewRun} sx={{ ml: 1 }}>
            New run
          </Button>
        )}
      </Box>

      <Box sx={{ display: 'flex', gap: 2, flexWrap: 'wrap', mb: 2, alignItems: 'center' }}>
        <FormControl size="small" sx={{ minWidth: 200 }}>
          <InputLabel id="status-filter-label">Status</InputLabel>
          <Select
            labelId="status-filter-label"
            label="Status"
            multiple
            value={statusFilter}
            onChange={handleStatusChange}
            renderValue={(selected) => (
              <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 0.5 }}>
                {selected.map((value) => (
                  <Chip key={value} label={value} size="small" />
                ))}
              </Box>
            )}
          >
            {RUN_STATUSES.map((status) => (
              <MenuItem key={status} value={status}>
                {status}
              </MenuItem>
            ))}
          </Select>
        </FormControl>

        <FormControl size="small" sx={{ minWidth: 200 }}>
          <InputLabel id="change-filter-label">Changed repo</InputLabel>
          <Select
            labelId="change-filter-label"
            label="Changed repo"
            value={changeFilter}
            onChange={(e) => setChangeFilter(e.target.value)}
          >
            <MenuItem value="">All repos</MenuItem>
            {repos.map((repo) => (
              <MenuItem key={repo} value={repo}>
                {repo}
              </MenuItem>
            ))}
          </Select>
        </FormControl>

        <Button
          variant="outlined"
          size="small"
          onClick={clearFilters}
          disabled={statusFilter.length === 0 && changeFilter === ''}
        >
          Clear filters
        </Button>
      </Box>

      {loading && <LinearProgress sx={{ mb: 2 }} />}

      {runs.length === 0 && !loading && (
        <Alert severity="info">
          {allRuns.length === 0
            ? 'No runs yet. Click "New run" to trigger a pipeline.'
            : 'No runs match the selected filters.'}
        </Alert>
      )}

      <List>
        {runs.map((run, i) => {
          const st = formatStatus(run.status);
          return (
            <Box key={run.id}>
              {i > 0 && <Divider />}
              <ListItem sx={{ cursor: 'pointer', '&:hover': { bgcolor: 'action.hover' } }}
                onClick={() => onOpenRun?.(run)}>
                <ListItemIcon><HistoryIcon /></ListItemIcon>
                <ListItemText
                  primary={
                    <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                      <Chip label={`#${run.id}`} size="small" />
                      <Chip label={st.label} color={st.color} size="small" variant="outlined" />
                      <Typography variant="subtitle2" fontWeight={600}>{run.project_name}</Typography>
                    </Box>
                  }
                  secondary={
                    <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mt: 0.5 }}>
                      <Typography variant="caption" color="text.secondary">
                        {new Date(run.started_at).toLocaleString()} by {actorLabel(run.started_by)}
                        {run.finished_at && ` → ${new Date(run.finished_at).toLocaleString()}`}
                      </Typography>
                      <Typography variant="caption" color="text.secondary">
                        {run.rerun_task
                          ? `Re-run: ${run.rerun_task}`
                          : run.changed?.length ? `Changed: ${run.changed.join(', ')}` : 'No changes'}
                      </Typography>
                    </Box>
                  }
                />
              </ListItem>
            </Box>
          );
        })}
      </List>

      {pagination && (
        <Pagination
          pagination={pagination}
          onPageChange={handlePageChange}
          onLimitChange={handleLimitChange}
        />
      )}
    </Box>
  );
}

export function NewRun({ onSelect, onBack }) {
  const [projects, setProjects] = useState([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    listProjects()
      .then(data => setProjects(data.items || []))
      .catch(() => {})
      .finally(() => setLoading(false));
  }, []);

  return (
    <Box>
      <Box sx={{ display: 'flex', alignItems: 'center', mb: 3 }}>
        <Button size="small" startIcon={<BackIcon />} onClick={onBack}>Back to runs</Button>
        <Typography variant="h4" sx={{ ml: 2 }}>New Run</Typography>
      </Box>
      <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
        Select a project to view its pipeline and trigger a run.
      </Typography>
      {loading && <LinearProgress sx={{ mb: 2 }} />}
      <List>
        {projects.map((p, i) => (
          <Box key={p.name}>
            {i > 0 && <Divider />}
            <ListItem sx={{ cursor: 'pointer', '&:hover': { bgcolor: 'action.hover' } }}
              onClick={() => onSelect(p.name)}>
              <ListItemIcon><DagIcon /></ListItemIcon>
			  <ListItemText primary={p.name} secondary={`${p.repo_url || 'No remote configured'}${p.repo_ref ? ` @ ${p.repo_ref}` : ''}`} />
            </ListItem>
          </Box>
        ))}
      </List>
    </Box>
  );
}

export function RunProject({ projectName, onBack, onStarted }) {
  const { roleAtLeast } = useAuth();
  const canOperate = roleAtLeast('operator');
  const [project, setProject] = useState(null);
  const [nodes, setNodes, onNodesChange] = useNodesState([]);
  const [edges, setEdges, onEdgesChange] = useEdgesState([]);
  const [changedRepos, setChangedRepos] = useState('');
  const [resolvedTasks, setResolvedTasks] = useState([]);
  const [includeManual, setIncludeManual] = useState(false);
  const [running, setRunning] = useState(false);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);
  const [showPipeline, setShowPipeline] = useState(false);
  const [hclText, setHclText] = useState('');

  useEffect(() => {
    setLoading(true);
    setError(null);
    getProjectDefinition(projectName)
      .then(proj => {
        setProject(proj);
        const { nodes: n, edges: e } = layoutFromProject(proj);
        setNodes(n);
        setEdges(e);
        return getProjectPipeline(projectName);
      })
      .then(raw => setHclText(raw || ''))
      .catch(e => setError(e.message))
      .finally(() => setLoading(false));
  }, [projectName]);

  const handleResolve = async () => {
    const changed = changedRepos.split(',').map(s => s.trim()).filter(Boolean);
    try {
      const result = await resolveProject(projectName, changed);
      const triggered = result.triggered || [];
      setResolvedTasks(triggered);
      const triggeredSet = new Set(triggered.map(t => t.name));
      setNodes(prev => prev.map(node => {
        const taskName = node.id.replace('task:', '');
        if (node.data.type === 'task') {
          const isOn = triggeredSet.has(taskName);
          return {
            ...node,
            style: { ...node.style, border: isOn ? `2px solid ${node.data.manual ? '#42a5f5' : '#4caf50'}` : '2px solid #f44336', opacity: isOn ? 1 : 0.45 },
          };
        }
        return node;
      }));
    } catch (e) {
      setError(e.message);
    }
  };

  const handleRun = async () => {
    setRunning(true);
    setError(null);
    try {
      const changed = changedRepos.split(',').map(s => s.trim()).filter(Boolean);
      const result = await runProject(projectName, changed, includeManual);
      if (result.run_id && onStarted) {
        onStarted(result.run_id);
        return;
      }
      setError('Run started but no run_id returned');
    } catch (e) {
      setError(e.message);
    }
    setRunning(false);
  };

  return (
    <Box sx={{ height: 'calc(100vh - 100px)' }}>
      <Box sx={{ display: 'flex', alignItems: 'center', mb: 2, gap: 2 }}>
        <Button size="small" startIcon={<BackIcon />} onClick={onBack}>Change project</Button>
        <Typography variant="h4">Pipeline: {projectName}</Typography>
        <Chip icon={<DagIcon />} label={`${project?.Repos?.length ?? 0} repos, ${project?.Tasks?.length ?? 0} tasks`} />
        <Box sx={{ flexGrow: 1 }} />
        <IconButton size="small" onClick={() => setShowPipeline(true)} title="View pipeline.tf">
          <ViewIcon />
        </IconButton>
      </Box>

      {loading && <LinearProgress sx={{ mb: 2 }} />}
      {error && <Alert severity="error" sx={{ mb: 1 }}>{error}</Alert>}

      <Box sx={{ display: 'flex', gap: 2, height: 'calc(100% - 60px)' }}>
        <Card sx={{ flex: 1, display: 'flex', flexDirection: 'column' }}>
          <CardContent sx={{ flex: 1, display: 'flex', flexDirection: 'column', p: 1, '&:last-child': { pb: 1 } }}>
            <Box sx={{ display: 'flex', gap: 1, mb: 1, alignItems: 'center' }}>
              <TextField size="small" placeholder="Changed repos: tf_plugin, backend" value={changedRepos}
                onChange={(e) => setChangedRepos(e.target.value)} sx={{ flex: 1 }} />
              <Button variant="contained" startIcon={<ResolveIcon />} onClick={handleResolve} disabled={!projectName}>Resolve</Button>
              {canOperate && (
                <Button color="success" variant="contained" startIcon={<RunIcon />} onClick={handleRun} disabled={running}>
                  {running ? 'Starting…' : 'Run'}
                </Button>
              )}
            </Box>
            <FormControlLabel
              sx={{ mb: 1, ml: 0 }}
              control={<Switch size="small" checked={includeManual} onChange={(e) => setIncludeManual(e.target.checked)} />}
              label="Auto-run manual jobs"
            />

            <DAGViewer nodes={nodes} edges={edges} />

            {resolvedTasks.length > 0 && (
              <Box sx={{ mt: 1 }}>
                <Typography variant="subtitle2" gutterBottom>Resolved Tasks</Typography>
                <Box sx={{ display: 'flex', gap: 1, flexWrap: 'wrap' }}>
                  {resolvedTasks.map(t => (
                    <Chip key={t.name} icon={<TriggeredIcon />} label={t.name} color="success" variant="filled" size="small" />
                  ))}
                </Box>
              </Box>
            )}
          </CardContent>
        </Card>
      </Box>

      <Dialog open={showPipeline} onClose={() => setShowPipeline(false)} maxWidth="md" fullWidth>
        <DialogTitle sx={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
          pipeline.tf
          <Box sx={{ display: 'flex', gap: 1, alignItems: 'center' }}>
			<Typography variant="caption" color="text.secondary">Read-only · edit and push in Git</Typography>
            <IconButton onClick={() => setShowPipeline(false)} size="small"><CloseIcon /></IconButton>
          </Box>
        </DialogTitle>
		<DialogContent sx={{ p: 0 }}>
		  <Alert severity="info" sx={{ m: 1 }}>This definition comes from the selected remote revision and cannot be edited here.</Alert>
          <Box sx={{ height: 500 }}>
            <CodeMirror
              value={hclText}
			  editable={false}
              theme={oneDark}
              height="100%"
              basicSetup={{ lineNumbers: true, foldGutter: true, highlightActiveLine: true }}
            />
          </Box>
        </DialogContent>
      </Dialog>
    </Box>
  );
}
