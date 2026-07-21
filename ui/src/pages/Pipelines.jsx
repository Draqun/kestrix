import { useState, useEffect, useCallback, useMemo } from 'react';
import { useSearchParams } from 'react-router-dom';
import {
  ReactFlow, Background, Controls, MiniMap,
  useNodesState, useEdgesState, MarkerType, Handle, Position
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
  Save as SaveIcon,
  CheckCircle as TriggeredIcon,
  AccountTree as DagIcon,
  ArrowBack as BackIcon,
  Close as CloseIcon,
  History as HistoryIcon,
  Visibility as ViewIcon,
  Add as AddIcon,
} from '@mui/icons-material';
import CodeMirror from '@uiw/react-codemirror';
import { oneDark } from '@codemirror/theme-one-dark';
import dagre from 'dagre';
import { getProjectPipeline, getProjectDefinition, saveProjectPipeline, resolveProject, runProject, listProjects, listAllRuns, getRun, cancelRun, approveManualTask } from '../api/client';
import { filterRuns, getUniqueRepos, RUN_STATUSES } from '../lib/runFilters.js';
import Pagination from '../components/Pagination';

function layoutFromProject(project) {
  const g = new dagre.graphlib.Graph();
  g.setDefaultEdgeLabel(() => ({}));
  g.setGraph({ rankdir: 'LR', nodesep: 80, ranksep: 120 });

  project.Repos?.forEach(repo => {
    g.setNode(`repo:${repo.Name}`, { width: 170, height: 60 });
  });
  project.Tasks?.forEach(task => {
    g.setNode(`task:${task.Name}`, { width: 190, height: 60 });
  });

  project.Tasks?.forEach(task => {
    const trig = task.Trigger || '';
    const reposInTrigger = [...trig.matchAll(/repo\.([a-zA-Z0-9_]+)\.changed/g)].map(m => m[1]);
    reposInTrigger.forEach(rn => {
      if (project.Repos?.some(r => r.Name === rn)) {
        g.setEdge(`repo:${rn}`, `task:${task.Name}`);
      }
    });
    if (task.Execute?.Repo) {
      g.setEdge(`task:${task.Name}`, `repo:${task.Execute.Repo}`);
    }
    task.DependsOn?.forEach(depName => {
      g.setNode(`task:${depName}`, { width: 190, height: 60 });
      g.setEdge(`task:${depName}`, `task:${task.Name}`);
    });
  });

  dagre.layout(g);

  const nodes = [];
  const edges = [];

  project.Repos?.forEach(repo => {
    const pos = g.node(`repo:${repo.Name}`) || { x: 100, y: 100 };
    nodes.push({
      id: `repo:${repo.Name}`,
      position: { x: pos.x - 85, y: pos.y - 30 },
      data: { label: `📦 ${repo.Name}`, type: 'repo', path: repo.Path, kind: repo.Kind },
      type: 'custom',
      style: { background: '#1e4976', border: '2px solid #2196f3', borderRadius: 8 },
    });
  });

  project.Tasks?.forEach(task => {
    const pos = g.node(`task:${task.Name}`) || { x: 200, y: 100 };
    nodes.push({
      id: `task:${task.Name}`,
      position: { x: pos.x - 95, y: pos.y - 30 },
      data: { label: `⚙️ ${task.Name}`, type: 'task', manual: task.Manual, trigger: task.Trigger, script: task.Execute?.Script },
      type: 'custom',
      style: { background: '#1a3a2a', border: `2px solid ${task.Manual ? '#42a5f5' : '#4caf50'}`, borderRadius: 8 },
    });
  });

  project.Tasks?.forEach(task => {
    const trig = task.Trigger || '';
    const reposInTrigger = [...trig.matchAll(/repo\.([a-zA-Z0-9_]+)\.changed/g)].map(m => m[1]);
    reposInTrigger.forEach(rn => {
      if (project.Repos?.some(r => r.Name === rn)) {
        edges.push({
          id: `e-trig-${rn}-${task.Name}`,
          source: `repo:${rn}`,
          target: `task:${task.Name}`,
          animated: true,
          markerEnd: { type: MarkerType.ArrowClosed },
          style: { stroke: '#ff9800', strokeWidth: 1.5 },
        });
      }
    });
    if (task.Execute?.Repo) {
      edges.push({
        id: `e-exec-${task.Name}-${task.Execute.Repo}`,
        source: `task:${task.Name}`,
        target: `repo:${task.Execute.Repo}`,
        animated: true,
        markerEnd: { type: MarkerType.ArrowClosed },
        style: { stroke: '#2196f3', strokeWidth: 1.5 },
      });
    }
    task.DependsOn?.forEach(depName => {
      edges.push({
        id: `e-dep-${depName}-${task.Name}`,
        source: `task:${depName}`,
        target: `task:${task.Name}`,
        animated: false,
        markerEnd: { type: MarkerType.ArrowClosed },
        style: { stroke: '#9c27b0', strokeWidth: 2.5 },
      });
    });
  });

  return { nodes, edges };
}

function CustomNode({ data }) {
  const statusColor = data.runStatus === 'passed' ? '#4caf50'
    : data.runStatus === 'failed' ? '#f44336'
    : data.runStatus === 'skipped' ? '#757575'
    : data.runStatus === 'running' ? '#ff9800'
    : data.runStatus === 'pending' ? '#616161'
    : data.runStatus === 'canceled' ? '#ff9800'
    : data.runStatus === 'manual' ? '#42a5f5'
    : null;
  const borderColor = statusColor || (data.type === 'repo' ? '#2196f3' : data.manual ? '#42a5f5' : '#4caf50');

  return (
    <Box sx={{
      background: data.type === 'repo' ? '#1e4976' : '#1a3a2a',
      border: `2px solid ${borderColor}`,
      borderRadius: 2, px: 2, py: 1, minWidth: 130, textAlign: 'center',
      position: 'relative',
      opacity: data.runStatus === 'skipped' || data.runStatus === 'pending' || data.runStatus === 'canceled' ? 0.5 : 1,
    }}>
      <Handle type="target" position={Position.Left} style={{ background: '#fff', width: 8, height: 8 }} />
      <Typography variant="body2" fontWeight={600}>{data.label}</Typography>
      {data.path && <Typography variant="caption" color="text.secondary">{data.path}</Typography>}
      {data.trigger && (
        <Typography variant="caption" color="warning.main" display="block" sx={{ fontSize: 10, wordBreak: 'break-word' }}>
          {data.trigger}
        </Typography>
      )}
      {data.manual && !data.runStatus && (
        <Typography variant="caption" display="block" sx={{ fontSize: 9, fontWeight: 600, mt: 0.5, color: '#42a5f5' }}>
          MANUAL
        </Typography>
      )}
      {data.runStatus && data.runStatus !== 'passed' && (
        <Typography variant="caption" display="block" sx={{ fontSize: 9, fontWeight: 700, mt: 0.5, color: borderColor }}>
          {data.runStatus.toUpperCase()}
        </Typography>
      )}
      {data.runStatus === 'passed' && (
        <Typography variant="caption" display="block" sx={{ fontSize: 9, fontWeight: 700, mt: 0.5, color: '#4caf50' }}>
          PASSED
        </Typography>
      )}
      <Handle type="source" position={Position.Right} style={{ background: '#fff', width: 8, height: 8 }} />
    </Box>
  );
}

const nodeTypes = { custom: CustomNode };

function formatStatus(status) {
  if (status === 'passed') return { color: 'success', label: 'PASSED' };
  if (status === 'failed') return { color: 'error', label: 'FAILED' };
  if (status === 'canceled') return { color: 'warning', label: 'CANCELED' };
  if (status === 'manual') return { color: 'info', label: 'MANUAL' };
  if (status === 'skipped') return { color: 'default', label: 'SKIPPED' };
  if (status === 'pending') return { color: 'default', label: 'PENDING' };
  return { color: 'warning', label: 'RUNNING' };
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

function formatDuration(ms) {
  if (!ms) return '';
  if (ms < 1000) return `${ms}ms`;
  return `${(ms / 1000).toFixed(1)}s`;
}

export function RunDetail({ projectName, runId, onBack }) {
  const [detail, setDetail] = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);
  const [selectedJob, setSelectedJob] = useState(null);
  const [dagNodes, setDagNodes, onDagNodesChange] = useNodesState([]);
  const [dagEdges, setDagEdges, onDagEdgesChange] = useEdgesState([]);
  const [canceling, setCanceling] = useState(false);
  const [approving, setApproving] = useState(null);
  const [approveErr, setApproveErr] = useState(null);

  const load = useCallback(async () => {
    try {
      const d = await getRun(projectName, runId);
      setDetail(d);
      setError(null);
      return d;
    } catch (e) {
      setError(e.message);
      return null;
    }
  }, [projectName, runId]);

  useEffect(() => {
    setLoading(true);
    setSelectedJob(null);
    load().finally(() => setLoading(false));
  }, [load]);

  // Poll while running or a job is running after manual approve.
  useEffect(() => {
    if (!detail) return;
    const jobs = detail.task_results || [];
    const busy = detail.status === 'running' || jobs.some(j => j.status === 'running');
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

  const r = detail;
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
        {r?.changed?.length > 0 && (
          <Chip label={`Changed: ${r.changed.join(', ')}`} size="small" variant="outlined" />
        )}
        <Box sx={{ flexGrow: 1 }} />
        {r?.status === 'running' && (
          <Button variant="outlined" color="warning" size="small" onClick={handleCancel} disabled={canceling}>
            {canceling ? 'Canceling…' : 'Cancel'}
          </Button>
        )}
      </Box>

      {loading && !r && <LinearProgress sx={{ mb: 2 }} />}
      {error && <Alert severity="error" sx={{ mb: 1 }}>{error}</Alert>}
      {approveErr && <Alert severity="error" sx={{ mb: 1 }} onClose={() => setApproveErr(null)}>{approveErr}</Alert>}

      {r && (
        <Typography variant="caption" color="text.secondary" display="block" sx={{ mb: 1 }}>
          {new Date(r.started_at).toLocaleString()}
          {r.finished_at && ` → ${new Date(r.finished_at).toLocaleString()}`}
        </Typography>
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
                          primaryTypographyProps={{ variant: 'body2', fontWeight: 600 }}
                          secondaryTypographyProps={{ variant: 'caption' }}
                        />
                      </ListItemButton>
                      {canRun && (
                        <Button size="small" variant="contained" color="info" sx={{ mt: 0.5, mx: 1 }}
                          disabled={!!approving} onClick={() => handleApprove(job.name)}>
                          {approving === job.name ? 'Starting…' : 'Run'}
                        </Button>
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
                  {isManualWaiting(activeJob, r?.pipeline_def, jobs) && (
                    <Button size="small" variant="contained" color="info" disabled={!!approving}
                      onClick={() => handleApprove(activeJob.name)}>
                      {approving === activeJob.name ? 'Starting…' : 'Run manual job'}
                    </Button>
                  )}
                </Box>
                {activeJob.error && activeJob.status !== 'manual' && (
                  <Alert severity="error" sx={{ mb: 1, mx: 1 }}>{activeJob.error}</Alert>
                )}
                {isManualWaiting(activeJob, r?.pipeline_def, jobs) && (
                  <Alert severity="info" sx={{ mb: 1, mx: 1 }}>Waiting for manual approval.</Alert>
                )}
                <Box component="pre" sx={{ m: 0, p: 1.5, flex: 1, overflow: 'auto', bgcolor: 'grey.900', borderRadius: 1, fontSize: 12, whiteSpace: 'pre-wrap' }}>
                  {activeJob.output || (activeJob.status === 'running' ? 'Running…' : '(No output)')}
                </Box>
            </>
            ) : (
              <Box sx={{ flex: 1, borderRadius: 1, overflow: 'hidden', minHeight: 320 }}>
                {dagNodes.length > 0 ? (
                  <ReactFlow nodes={dagNodes} edges={dagEdges}
                    onNodesChange={onDagNodesChange} onEdgesChange={onDagEdgesChange}
                    nodeTypes={nodeTypes} fitView proOptions={{ hideAttribution: true }}
                    onNodeClick={(_, node) => {
                      if (node.data?.type === 'task') setSelectedJob(node.id.replace('task:', ''));
                    }}>
                    <Background color="#1e3a5f" />
                    <Controls />
                    <MiniMap nodeColor={(n) => n.data?.type === 'repo' ? '#2196f3' : '#4caf50'} maskColor="rgba(0,0,0,0.7)" />
                  </ReactFlow>
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
              <Box component="pre" sx={{ m: 0, mt: 1, p: 1.5, maxHeight: 160, overflow: 'auto', bgcolor: 'grey.900', borderRadius: 1, fontSize: 11, whiteSpace: 'pre-wrap' }}>
                {r.output}
              </Box>
            )}
          </CardContent>
        </Card>
      </Box>
    </Box>
  );
}

export function RunList({ onNewRun, onOpenRun }) {
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
        <Button variant="contained" startIcon={<AddIcon />} onClick={onNewRun} sx={{ ml: 1 }}>
          New run
        </Button>
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
                        {new Date(run.started_at).toLocaleString()}
                        {run.finished_at && ` → ${new Date(run.finished_at).toLocaleString()}`}
                      </Typography>
                      <Typography variant="caption" color="text.secondary">
                        {run.changed?.length ? `Changed: ${run.changed.join(', ')}` : 'No changes'}
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
              <ListItemText primary={p.name} secondary={p.repo_path || p.pipeline_dir || 'ci'} />
            </ListItem>
          </Box>
        ))}
      </List>
    </Box>
  );
}

export function RunProject({ projectName, onBack, onStarted }) {
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
  const [saving, setSaving] = useState(false);
  const [saveMsg, setSaveMsg] = useState(null);

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

  const handleSave = async () => {
    setSaving(true);
    setSaveMsg(null);
    setError(null);
    try {
      const res = await saveProjectPipeline(projectName, hclText);
      setSaveMsg(`Saved to ${res.path}`);
    } catch (e) {
      setError(e.message);
    }
    setSaving(false);
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
              <Button color="success" variant="contained" startIcon={<RunIcon />} onClick={handleRun} disabled={running}>
                {running ? 'Starting…' : 'Run'}
              </Button>
            </Box>
            <FormControlLabel
              sx={{ mb: 1, ml: 0 }}
              control={<Switch size="small" checked={includeManual} onChange={(e) => setIncludeManual(e.target.checked)} />}
              label="Auto-run manual jobs"
            />

            <Box sx={{ flex: 1, borderRadius: 1, overflow: 'hidden' }}>
              <ReactFlow nodes={nodes} edges={edges} onNodesChange={onNodesChange} onEdgesChange={onEdgesChange}
                nodeTypes={nodeTypes} fitView proOptions={{ hideAttribution: true }}>
                <Background color="#1e3a5f" />
                <Controls />
                <MiniMap nodeColor={(n) => n.data?.type === 'repo' ? '#2196f3' : '#4caf50'} maskColor="rgba(0,0,0,0.7)" />
              </ReactFlow>
            </Box>

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
            {saveMsg && <Typography variant="caption" color="success.main">{saveMsg}</Typography>}
            <Button variant="contained" startIcon={<SaveIcon />} onClick={handleSave} disabled={saving} size="small">
              {saving ? 'Saving…' : 'Save'}
            </Button>
            <IconButton onClick={() => setShowPipeline(false)} size="small"><CloseIcon /></IconButton>
          </Box>
        </DialogTitle>
        <DialogContent sx={{ p: 0 }}>
          <Box sx={{ height: 500 }}>
            <CodeMirror
              value={hclText}
              onChange={(val) => setHclText(val)}
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
