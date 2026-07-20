import { useState, useEffect, useCallback } from 'react';
import {
  ReactFlow, Background, Controls, MiniMap,
  useNodesState, useEdgesState, MarkerType, Handle, Position
} from 'reactflow';
import 'reactflow/dist/style.css';
import {
  Box, Card, CardContent, Typography, Button, TextField, Chip, Autocomplete,
  Alert, LinearProgress, FormControlLabel, Switch,
  Dialog, DialogTitle, DialogContent, DialogActions, IconButton, Divider,
  List, ListItem, ListItemText, ListItemIcon, ListItemSecondaryAction,
} from '@mui/material';
import {
  PlayArrow as ResolveIcon,
  RocketLaunch as RunIcon,
  Save as SaveIcon,
  CheckCircle as TriggeredIcon,
  AccountTree as DagIcon,
  Code as CodeIcon,
  ArrowBack as BackIcon,
  Close as CloseIcon,
  History as HistoryIcon,
  Visibility as ViewIcon,
  Add as AddIcon,
} from '@mui/icons-material';
import CodeMirror from '@uiw/react-codemirror';
import { oneDark } from '@codemirror/theme-one-dark';
import dagre from 'dagre';
import { getProjectPipeline, getProjectDefinition, saveProjectPipeline, resolveProject, runProject, listProjects, listRuns, listAllRuns, getRun } from '../api/client';

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
  return (
    <Box sx={{
      background: data.type === 'repo' ? '#1e4976' : '#1a3a2a',
      border: `2px solid ${data.type === 'repo' ? '#2196f3' : data.manual ? '#ff9800' : '#4caf50'}`,
      borderRadius: 2, px: 2, py: 1, minWidth: 130, textAlign: 'center',
      position: 'relative',
    }}>
      <Handle type="target" position={Position.Left} style={{ background: '#fff', width: 8, height: 8 }} />
      <Typography variant="body2" fontWeight={600}>{data.label}</Typography>
      {data.path && <Typography variant="caption" color="text.secondary">{data.path}</Typography>}
      {data.trigger && (
        <Typography variant="caption" color="warning.main" display="block" sx={{ fontSize: 10, wordBreak: 'break-word' }}>
          {data.trigger}
        </Typography>
      )}
      {data.manual && (
        <Typography variant="caption" color="warning.main" display="block" sx={{ fontSize: 9, fontWeight: 600, mt: 0.5 }}>
          MANUAL
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

function RunDetailView({ run, onClose }) {
  const [detail, setDetail] = useState(null);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    if (!run) { setDetail(null); return; }
    setLoading(true);
    getRun(run.project_name, run.id)
      .then(d => setDetail(d))
      .catch(() => setDetail(run))
      .finally(() => setLoading(false));
  }, [run]);

  const r = detail || run;
  if (!r) return null;
  const st = formatStatus(r.status);

  return (
    <Dialog open={!!run} onClose={onClose} maxWidth="md" fullWidth>
      <DialogTitle sx={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
        Run #{r.id} — {r.project_name}
        <IconButton onClick={onClose} size="small"><CloseIcon /></IconButton>
      </DialogTitle>
      <DialogContent>
        {loading && <LinearProgress sx={{ mb: 2 }} />}
        <Box sx={{ display: 'flex', gap: 1, mb: 2 }}>
          <Chip label={st.label} color={st.color} />
          <Chip label={`Changed: ${r.changed?.join(', ') || 'none'}`} variant="outlined" />
          <Typography variant="caption" color="text.secondary" sx={{ ml: 'auto', alignSelf: 'center' }}>
            {new Date(r.started_at).toLocaleString()}
            {r.finished_at && ` → ${new Date(r.finished_at).toLocaleString()}`}
          </Typography>
        </Box>
        {r.error && <Alert severity="error" sx={{ mb: 1 }}>{r.error}</Alert>}
        <Box component="pre" sx={{ p: 1.5, maxHeight: 400, overflow: 'auto', bgcolor: 'grey.900', borderRadius: 1, fontSize: 12, whiteSpace: 'pre-wrap' }}>
          {r.output || '(No output)'}
        </Box>
      </DialogContent>
    </Dialog>
  );
}

function NewRunView({ onBack }) {
  const [projects, setProjects] = useState([]);
  const [selectedProject, setSelectedProject] = useState(null);
  const [project, setProject] = useState(null);
  const [nodes, setNodes, onNodesChange] = useNodesState([]);
  const [edges, setEdges, onEdgesChange] = useEdgesState([]);
  const [changedRepos, setChangedRepos] = useState('');
  const [resolvedTasks, setResolvedTasks] = useState([]);
  const [includeManual, setIncludeManual] = useState(true);
  const [running, setRunning] = useState(false);
  const [runResult, setRunResult] = useState(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState(null);
  const [showPipeline, setShowPipeline] = useState(false);
  const [hclText, setHclText] = useState('');
  const [saving, setSaving] = useState(false);
  const [saveMsg, setSaveMsg] = useState(null);

  useEffect(() => {
    listProjects().then(data => setProjects(Array.isArray(data) ? data : [])).catch(() => {});
  }, []);

  const loadProject = useCallback(async (name) => {
    setLoading(true);
    setError(null);
    try {
      const proj = await getProjectDefinition(name);
      setProject(proj);
      const { nodes: n, edges: e } = layoutFromProject(proj);
      setNodes(n);
      setEdges(e);
      const raw = await getProjectPipeline(name);
      setHclText(raw || '');
    } catch (e) {
      setError(e.message);
    }
    setLoading(false);
  }, [setNodes, setEdges]);

  useEffect(() => {
    if (selectedProject) loadProject(selectedProject);
  }, [selectedProject, loadProject]);

  const handleResolve = async () => {
    if (!selectedProject) return;
    const changed = changedRepos.split(',').map(s => s.trim()).filter(Boolean);
    try {
      const result = await resolveProject(selectedProject, changed);
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
    if (!selectedProject) return;
    setRunning(true);
    setError(null);
    setRunResult(null);
    try {
      const changed = changedRepos.split(',').map(s => s.trim()).filter(Boolean);
      const result = await runProject(selectedProject, changed, includeManual);
      setRunResult(result);
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
      const res = await saveProjectPipeline(selectedProject, hclText);
      setSaveMsg(`Saved to ${res.path}`);
    } catch (e) {
      setError(e.message);
    }
    setSaving(false);
  };

  if (!selectedProject) {
    return (
      <Box>
        <Box sx={{ display: 'flex', alignItems: 'center', mb: 3 }}>
          <Button size="small" startIcon={<BackIcon />} onClick={onBack}>Back to runs</Button>
          <Typography variant="h4" sx={{ ml: 2 }}>New Run</Typography>
        </Box>
        <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
          Select a project to view its pipeline and trigger a run.
        </Typography>
        <List>
          {projects.map((p, i) => (
            <Box key={p.name}>
              {i > 0 && <Divider />}
              <ListItem sx={{ cursor: 'pointer', '&:hover': { bgcolor: 'action.hover' } }}
                onClick={() => setSelectedProject(p.name)}>
                <ListItemIcon><DagIcon /></ListItemIcon>
                <ListItemText primary={p.name} secondary={p.repo_path || p.pipeline_dir || 'ci'} />
              </ListItem>
            </Box>
          ))}
        </List>
      </Box>
    );
  }

  return (
    <Box sx={{ height: 'calc(100vh - 100px)' }}>
      <Box sx={{ display: 'flex', alignItems: 'center', mb: 2, gap: 2 }}>
        <Button size="small" startIcon={<BackIcon />} onClick={() => setSelectedProject(null)}>Change project</Button>
        <Typography variant="h4">Pipeline: {selectedProject}</Typography>
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
              <Button variant="contained" startIcon={<ResolveIcon />} onClick={handleResolve} disabled={!selectedProject}>Resolve</Button>
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
                <Alert severity={runResult.status === 'passed' ? 'success' : 'error'} sx={{ mb: 1 }}>
                  {runResult.status === 'passed' ? 'Pipeline passed.' : `Pipeline failed: ${runResult.error}`}
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

export default function Pipelines() {
  const [view, setView] = useState('list');
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

  if (view === 'new') {
    return <NewRunView onBack={() => { setView('list'); loadRuns(); }} />;
  }

  return (
    <Box>
      <Box sx={{ display: 'flex', alignItems: 'center', mb: 3 }}>
        <Typography variant="h4" sx={{ flexGrow: 1 }}>Runs</Typography>
        <IconButton onClick={loadRuns} title="Refresh">
          <HistoryIcon />
        </IconButton>
        <Button variant="contained" startIcon={<AddIcon />} onClick={() => setView('new')} sx={{ ml: 1 }}>
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
