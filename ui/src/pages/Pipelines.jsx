import { useState, useEffect, useCallback } from 'react';
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
import { getProjectPipeline, getProjectDefinition, saveProjectPipeline, resolveProject, runProject, listProjects, listAllRuns, getRun } from '../api/client';

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
      style: { background: '#1a3a2a', border: `2px solid ${task.Manual ? '#ff9800' : '#4caf50'}`, borderRadius: 8 },
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
    : null;
  const borderColor = statusColor || (data.type === 'repo' ? '#2196f3' : data.manual ? '#ff9800' : '#4caf50');

  return (
    <Box sx={{
      background: data.type === 'repo' ? '#1e4976' : '#1a3a2a',
      border: `2px solid ${borderColor}`,
      borderRadius: 2, px: 2, py: 1, minWidth: 130, textAlign: 'center',
      position: 'relative',
      opacity: data.runStatus === 'skipped' || data.runStatus === 'pending' ? 0.5 : 1,
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
        <Typography variant="caption" color="warning.main" display="block" sx={{ fontSize: 9, fontWeight: 600, mt: 0.5 }}>
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
  return { color: 'warning', label: 'RUNNING' };
}

function formatDuration(ms) {
  if (!ms) return '';
  if (ms < 1000) return `${ms}ms`;
  return `${(ms / 1000).toFixed(1)}s`;
}

function RunDetailView({ run, onClose }) {
  const [detail, setDetail] = useState(null);
  const [loading, setLoading] = useState(false);
  const [selectedJob, setSelectedJob] = useState(null);

  useEffect(() => {
    if (!run) { setDetail(null); setSelectedJob(null); return; }
    setLoading(true);
    setSelectedJob(null);
    getRun(run.project_name, run.id)
      .then(d => { setDetail(d); setSelectedJob(null); })
      .catch(() => { setDetail(run); setSelectedJob(null); })
      .finally(() => setLoading(false));
  }, [run]);

  const r = detail || run;
  if (!r) return null;
  const st = formatStatus(r.status);
  const jobs = r.task_results || [];
  const activeJob = selectedJob != null ? jobs.find(j => j.name === selectedJob) : null;

  return (
    <Dialog open={!!run} onClose={onClose} maxWidth="lg" fullWidth>
      <DialogTitle sx={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
        Run #{r.id} — {r.project_name}
        <IconButton onClick={onClose} size="small"><CloseIcon /></IconButton>
      </DialogTitle>
      <DialogContent sx={{ display: 'flex', gap: 2, minHeight: 400 }}>
        {loading && <LinearProgress sx={{ position: 'absolute', top: 0, left: 0, right: 0 }} />}
        {/* Job list */}
        <Box sx={{ width: 240, flexShrink: 0, borderRight: 1, borderColor: 'divider', pr: 2 }}>
          <Box sx={{ display: 'flex', gap: 0.5, mb: 2, flexWrap: 'wrap' }}>
            <Chip label={st.label} color={st.color} size="small" />
            <Chip label={`Changed: ${r.changed?.join(', ') || 'none'}`} size="small" variant="outlined" />
          </Box>
          <Typography variant="caption" color="text.secondary" display="block" sx={{ mb: 1 }}>
            {new Date(r.started_at).toLocaleString()}
            {r.finished_at && ` → ${new Date(r.finished_at).toLocaleString()}`}
          </Typography>
          {jobs.length > 0 ? (
            <List dense disablePadding>
              {jobs.map(job => {
                const js = formatStatus(job.status);
                return (
                  <ListItem key={job.name} disablePadding
                    sx={{ mb: 0.5 }}>
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
                  </ListItem>
                );
              })}
            </List>
          ) : (
            <Typography variant="body2" color="text.secondary">
              No job data. Run with PostgreSQL to see per-job logs.
            </Typography>
          )}
        </Box>

        {/* Job output */}
        <Box sx={{ flex: 1, minWidth: 0 }}>
          {activeJob ? (
            <>
              <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 1 }}>
                <Typography variant="subtitle1" fontWeight={700}>{activeJob.name}</Typography>
                <Chip label={formatStatus(activeJob.status).label} color={formatStatus(activeJob.status).color} size="small" />
                {activeJob.duration > 0 && (
                  <Typography variant="caption" color="text.secondary">{formatDuration(activeJob.duration)}</Typography>
                )}
              </Box>
              {activeJob.error && <Alert severity="error" sx={{ mb: 1 }}>{activeJob.error}</Alert>}
              <Box component="pre" sx={{ p: 1.5, maxHeight: 500, overflow: 'auto', bgcolor: 'grey.900', borderRadius: 1, fontSize: 12, whiteSpace: 'pre-wrap' }}>
                {activeJob.output || '(No output)'}
              </Box>
            </>
          ) : (
            <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100%' }}>
              <Typography variant="body2" color="text.secondary">
                {jobs.length > 0 ? 'Select a job to view its logs' : 'No output available'}
              </Typography>
            </Box>
          )}
        </Box>
      </DialogContent>
    </Dialog>
  );
}

export function RunList({ onNewRun }) {
  const [runs, setRuns] = useState([]);
  const [loading, setLoading] = useState(true);
  const [selectedRun, setSelectedRun] = useState(null);

  const loadRuns = useCallback(async () => {
    setLoading(true);
    try {
      const data = await listAllRuns();
      setRuns(Array.isArray(data) ? data : []);
    } catch { setRuns([]); }
    setLoading(false);
  }, []);

  useEffect(() => { loadRuns(); }, [loadRuns]);

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

      {loading && <LinearProgress sx={{ mb: 2 }} />}

      {runs.length === 0 && !loading && (
        <Alert severity="info">No runs yet. Click "New run" to trigger a pipeline.</Alert>
      )}

      <List>
        {runs.map((run, i) => {
          const st = formatStatus(run.status);
          return (
            <Box key={run.id}>
              {i > 0 && <Divider />}
              <ListItem sx={{ cursor: 'pointer', '&:hover': { bgcolor: 'action.hover' } }}
                onClick={() => setSelectedRun(run)}>
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
                        Changed: {run.changed?.join(', ') || 'none'}
                      </Typography>
                    </Box>
                  }
                />
              </ListItem>
            </Box>
          );
        })}
      </List>

      <RunDetailView run={selectedRun} onClose={() => setSelectedRun(null)} />
    </Box>
  );
}

export function NewRun({ onSelect, onBack }) {
  const [projects, setProjects] = useState([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    listProjects()
      .then(data => setProjects(Array.isArray(data) ? data : []))
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

export function RunProject({ projectName, onBack }) {
  const [project, setProject] = useState(null);
  const [nodes, setNodes, onNodesChange] = useNodesState([]);
  const [edges, setEdges, onEdgesChange] = useEdgesState([]);
  const [changedRepos, setChangedRepos] = useState('');
  const [resolvedTasks, setResolvedTasks] = useState([]);
  const [includeManual, setIncludeManual] = useState(true);
  const [running, setRunning] = useState(false);
  const [runResult, setRunResult] = useState(null);
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
            style: { ...node.style, border: isOn ? `2px solid ${node.data.manual ? '#ff9800' : '#4caf50'}` : '2px solid #f44336', opacity: isOn ? 1 : 0.45 },
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
    setRunResult(null);
    // Reset DAG node statuses.
    setNodes(prev => prev.map(node => {
      if (node.data.type === 'task') {
        return { ...node, data: { ...node.data, runStatus: undefined } };
      }
      return node;
    }));
    try {
      const changed = changedRepos.split(',').map(s => s.trim()).filter(Boolean);
      const result = await runProject(projectName, changed, includeManual);
      setRunResult(result);

      // If async (running), poll for completion.
      if (result.status === 'running' && result.run_id) {
        const pollInterval = setInterval(async () => {
          try {
            const run = await getRun(projectName, result.run_id);
            if (!run) return;
            // Update DAG nodes with current task_results on every poll.
            if (run.task_results && run.task_results.length > 0) {
              const resultMap = {};
              run.task_results.forEach(t => { resultMap[t.name] = t.status; });
              setNodes(prev => prev.map(node => {
                const taskName = node.id.replace('task:', '');
                if (node.data.type === 'task' && resultMap[taskName]) {
                  return { ...node, data: { ...node.data, runStatus: resultMap[taskName] } };
                }
                return node;
              }));
            }
            if (run.status !== 'running') {
              setRunResult(run);
              clearInterval(pollInterval);
              setRunning(false);
            }
          } catch { /* retry */ }
        }, 1500);
      } else {
        setRunning(false);
      }
    } catch (e) {
      setError(e.message);
      setRunning(false);
    }
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
                {running ? 'Running…' : 'Run'}
              </Button>
            </Box>
            <FormControlLabel
              sx={{ mb: 1, ml: 0 }}
              control={<Switch size="small" checked={includeManual} onChange={(e) => setIncludeManual(e.target.checked)} />}
              label="Include manual tasks"
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

            {runResult && (
              <Box sx={{ mt: 1 }}>
                <Alert severity={runResult.status === 'passed' ? 'success' : runResult.status === 'running' ? 'info' : 'error'} sx={{ mb: 1 }}>
                  {runResult.status === 'passed' ? 'Pipeline passed.' : runResult.status === 'running' ? 'Pipeline is running…' : `Pipeline failed: ${runResult.error}`}
                </Alert>
                <Box component="pre" sx={{ m: 0, p: 1.5, maxHeight: 200, overflow: 'auto', bgcolor: 'grey.900', borderRadius: 1, fontSize: 12, whiteSpace: 'pre-wrap' }}>
                  {runResult.output || '(No output)'}
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
