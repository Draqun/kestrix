import { useState, useEffect, useCallback } from 'react';
import {
  ReactFlow, Background, Controls, MiniMap,
  useNodesState, useEdgesState, MarkerType, Handle, Position
} from 'reactflow';
import 'reactflow/dist/style.css';
import {
  Box, Card, CardContent, Typography, Button, TextField, Chip, Autocomplete,
  Alert, LinearProgress, ToggleButton, ToggleButtonGroup, FormControlLabel, Switch,
  Dialog, DialogTitle, DialogContent, DialogActions, IconButton, Divider,
  Table, TableBody, TableCell, TableContainer, TableHead, TableRow, Paper,
} from '@mui/material';
import {
  PlayArrow as ResolveIcon,
  RocketLaunch as RunIcon,
  Save as SaveIcon,
  CheckCircle as TriggeredIcon,
  AccountTree as DagIcon,
  Code as CodeIcon,
  AccountTree as GraphIcon,
  ArrowBack as BackIcon,
  Close as CloseIcon,
  History as HistoryIcon,
  Visibility as ViewIcon,
} from '@mui/icons-material';
import CodeMirror from '@uiw/react-codemirror';
import { oneDark } from '@codemirror/theme-one-dark';
import dagre from 'dagre';
import { getProjectPipeline, getProjectDefinition, saveProjectPipeline, resolveProject, runProject, listProjects, listRuns } from '../api/client';

const EMPTY_TEMPLATE = `project "new-project" {
  default_image = "golang:1.26"

  ci_repo "tf_plugin" {
    path = "tf-plugin"
    kind = "subpath"
  }

  ci_task "build_tf_plugin" {
    trigger = "repo.tf_plugin.changed"
    execute {
      repo   = "tf_plugin"
      script = "make build"
    }
  }
}
`;

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

export default function PipelineEditor({ projectName, onProjectChange }) {
  const [hclText, setHclText] = useState(EMPTY_TEMPLATE);
  const [project, setProject] = useState(null);
  const [nodes, setNodes, onNodesChange] = useNodesState([]);
  const [edges, setEdges, onEdgesChange] = useEdgesState([]);
  const [changedRepos, setChangedRepos] = useState('');
  const [resolvedTasks, setResolvedTasks] = useState([]);

  const [running, setRunning] = useState(false);
  const [runResult, setRunResult] = useState(null);
  const [includeManual, setIncludeManual] = useState(true);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState(null);
  const [saveMsg, setSaveMsg] = useState(null);
  const [view, setView] = useState('graph');

  const [projectOptions, setProjectOptions] = useState([]);
  const [projectsLoading, setProjectsLoading] = useState(false);

  const [showPipeline, setShowPipeline] = useState(false);
  const [runs, setRuns] = useState([]);
  const [runsLoading, setRunsLoading] = useState(false);
  const [selectedRun, setSelectedRun] = useState(null);

  const loadPipeline = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const raw = await getProjectPipeline(projectName);
      setHclText(raw || EMPTY_TEMPLATE);
      const proj = await getProjectDefinition(projectName);
      setProject(proj);
      const { nodes: n, edges: e } = layoutFromProject(proj);
      setNodes(n);
      setEdges(e);
    } catch (e) {
      setError(e.message);
    }
    setLoading(false);
  }, [projectName, setNodes, setEdges]);

  const loadRuns = useCallback(async () => {
    if (!projectName) return;
    setRunsLoading(true);
    try {
      const data = await listRuns(projectName);
      setRuns(Array.isArray(data) ? data : []);
    } catch { setRuns([]); }
    setRunsLoading(false);
  }, [projectName]);

  useEffect(() => {
    if (projectName) {
      loadPipeline();
      loadRuns();
    }
  }, [projectName, loadPipeline, loadRuns]);

  const handleSave = async () => {
    setSaving(true);
    setSaveMsg(null);
    setError(null);
    try {
      const res = await saveProjectPipeline(projectName, hclText);
      setSaveMsg(`Saved to ${res.path}`);
      await loadPipeline();
    } catch (e) {
      setError(e.message);
    }
    setSaving(false);
  };

  const handleResolve = async () => {
    if (!projectName) return;
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
    if (!projectName) return;
    setRunning(true);
    setError(null);
    setRunResult(null);
    try {
      const changed = changedRepos.split(',').map(s => s.trim()).filter(Boolean);
      const result = await runProject(projectName, changed, includeManual);
      setRunResult(result);
      loadRuns();
    } catch (e) {
      setError(e.message);
    }
    setRunning(false);
  };

  if (!projectName) {
    return (
      <Box sx={{ p: 3, maxWidth: 480 }}>
        <Typography variant="h5" gutterBottom>Pipeline Editor</Typography>
        <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
          Select a project to edit its pipeline definition.
        </Typography>
        <Autocomplete
          options={projectOptions}
          loading={projectsLoading}
          onOpen={async () => {
            setProjectsLoading(true);
            try {
              const data = await listProjects();
              setProjectOptions(Array.isArray(data) ? data.map(p => p.name) : []);
            } catch { setProjectOptions([]); }
            setProjectsLoading(false);
          }}
          onChange={(_, val) => {
            if (val && onProjectChange) onProjectChange(val);
          }}
          isOptionEqualToValue={(opt, val) => opt === val}
          renderInput={(params) => (
            <TextField {...params} label="Choose project" placeholder="Search projects..." />
          )}
          fullWidth
        />
      </Box>
    );
  }
  if (loading) {
    return <Box sx={{ p: 3 }}><LinearProgress /></Box>;
  }

  return (
    <Box sx={{ height: 'calc(100vh - 100px)' }}>
      {/* Header */}
      <Box sx={{ display: 'flex', alignItems: 'center', mb: 2, gap: 2 }}>
        <Button size="small" startIcon={<BackIcon />} onClick={() => onProjectChange && onProjectChange(null)}>
          Change project
        </Button>
        <Typography variant="h4" sx={{ flexGrow: 0 }}>Pipeline: {projectName}</Typography>
        <Chip icon={<DagIcon />} label={`${project?.Repos?.length ?? 0} repos, ${project?.Tasks?.length ?? 0} tasks`} />
        <Box sx={{ flexGrow: 1 }} />
        <IconButton size="small" onClick={() => setShowPipeline(true)} title="View pipeline.tf">
          <ViewIcon />
        </IconButton>
        <IconButton size="small" onClick={loadRuns} title="Refresh runs">
          <HistoryIcon />
        </IconButton>
      </Box>

      <Box sx={{ display: 'flex', gap: 2, height: 'calc(100% - 60px)' }}>
        {/* DAG + Controls */}
        <Card sx={{ flex: 1, display: 'flex', flexDirection: 'column' }}>
          <CardContent sx={{ flex: 1, display: 'flex', flexDirection: 'column', p: 1, '&:last-child': { pb: 1 } }}>
            {/* Controls */}
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

            {/* DAG */}
            <Box sx={{ flex: 1, borderRadius: 1, overflow: 'hidden' }}>
              <ReactFlow nodes={nodes} edges={edges} onNodesChange={onNodesChange} onEdgesChange={onEdgesChange}
                nodeTypes={nodeTypes} fitView proOptions={{ hideAttribution: true }}>
                <Background color="#1e3a5f" />
                <Controls />
                <MiniMap nodeColor={(n) => n.data?.type === 'repo' ? '#2196f3' : '#4caf50'} maskColor="rgba(0,0,0,0.7)" />
              </ReactFlow>
            </Box>

            {/* Resolved tasks */}
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

            {/* Run output */}
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

        {/* Run History */}
        <Card sx={{ width: 320, display: 'flex', flexDirection: 'column' }}>
          <CardContent sx={{ flex: 1, display: 'flex', flexDirection: 'column', p: 1, '&:last-child': { pb: 1 }, overflow: 'hidden' }}>
            <Typography variant="subtitle2" sx={{ mb: 1 }}>Run History</Typography>
            {runsLoading ? <LinearProgress sx={{ mb: 1 }} /> : null}
            <Box sx={{ flex: 1, overflow: 'auto' }}>
              {runs.length === 0 && !runsLoading && (
                <Typography variant="body2" color="text.secondary">No runs yet</Typography>
              )}
              {runs.map(run => {
                const st = formatStatus(run.status);
                return (
                  <Paper key={run.id} sx={{ p: 1, mb: 1, cursor: 'pointer', '&:hover': { bgcolor: 'action.hover' } }}
                    onClick={() => setSelectedRun(run)}>
                    <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 0.5 }}>
                      <Chip label={`#${run.id}`} size="small" />
                      <Chip label={st.label} color={st.color} size="small" variant="outlined" />
                      <Typography variant="caption" color="text.secondary" sx={{ ml: 'auto' }}>
                        {new Date(run.started_at).toLocaleString()}
                      </Typography>
                    </Box>
                    <Typography variant="caption" color="text.secondary">
                      {run.changed?.join(', ') || '(no changes)'}
                    </Typography>
                  </Paper>
                );
              })}
            </Box>
          </CardContent>
        </Card>
      </Box>

      {/* Pipeline.tf popup */}
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
          {error && <Alert severity="error" sx={{ m: 1 }}>{error}</Alert>}
          {saveMsg && <Alert severity="success" sx={{ m: 1 }}>{saveMsg}</Alert>}
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

      {/* Run detail popup */}
      <Dialog open={!!selectedRun} onClose={() => setSelectedRun(null)} maxWidth="md" fullWidth>
        <DialogTitle sx={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
          Run #{selectedRun?.id} — {selectedRun?.project_name}
          <IconButton onClick={() => setSelectedRun(null)} size="small"><CloseIcon /></IconButton>
        </DialogTitle>
        <DialogContent>
          {selectedRun && (
            <>
              <Box sx={{ display: 'flex', gap: 1, mb: 2 }}>
                <Chip label={formatStatus(selectedRun.status).label} color={formatStatus(selectedRun.status).color} />
                <Chip label={`Changed: ${selectedRun.changed?.join(', ') || 'none'}`} variant="outlined" />
                <Typography variant="caption" color="text.secondary" sx={{ ml: 'auto', alignSelf: 'center' }}>
                  {new Date(selectedRun.started_at).toLocaleString()}
                  {selectedRun.finished_at && ` → ${new Date(selectedRun.finished_at).toLocaleString()}`}
                </Typography>
              </Box>
              {selectedRun.error && <Alert severity="error" sx={{ mb: 1 }}>{selectedRun.error}</Alert>}
              <Box component="pre" sx={{ p: 1.5, maxHeight: 400, overflow: 'auto', bgcolor: 'grey.900', borderRadius: 1, fontSize: 12, whiteSpace: 'pre-wrap' }}>
                {selectedRun.output || '(No output)'}
              </Box>
            </>
          )}
        </DialogContent>
      </Dialog>
    </Box>
  );
}
