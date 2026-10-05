import { useState, useEffect, useCallback } from 'react';
import {
  Box, Card, CardContent, Typography, Button, TextField, Chip, Autocomplete,
  Alert, LinearProgress, FormControlLabel, Switch,
  Dialog, DialogTitle, DialogContent, DialogActions, IconButton, Divider,
  Paper,
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
} from '@mui/icons-material';
import CodeMirror from '@uiw/react-codemirror';
import { oneDark } from '@codemirror/theme-one-dark';
import { useNodesState, useEdgesState } from 'reactflow';
import DAGViewer from '../components/DAGViewer';
import CopyButton from '../components/CopyButton';
import { layoutFromProject } from '../lib/dagLayout';
import { getProjectPipeline, getProjectDefinition, resolveProject, runProject, listProjects, listRuns } from '../api/client';

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
  const [error, setError] = useState(null);
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
      setRuns(data.items || []);
    } catch { setRuns([]); }
    setRunsLoading(false);
  }, [projectName]);

  useEffect(() => {
    if (projectName) {
      loadPipeline();
      loadRuns();
    }
  }, [projectName, loadPipeline, loadRuns]);

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
              setProjectOptions(data.items ? data.items.map(p => p.name) : []);
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
              label="Auto-run manual jobs"
            />

            {/* DAG */}
            <DAGViewer nodes={nodes} edges={edges} />

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
                <Box sx={{ position: 'relative' }}>
                  <CopyButton text={runResult.output || ''} />
                  <Box component="pre" sx={{ m: 0, p: 1.5, maxHeight: 200, overflow: 'auto', bgcolor: (theme) => theme.palette.mode === 'dark' ? 'grey.900' : 'grey.200', borderRadius: 1, fontSize: 12, whiteSpace: 'pre-wrap' }}>
                    {runResult.output || '(No output)'}
                  </Box>
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
                      {run.rerun_task ? `Re-run: ${run.rerun_task}` : run.changed?.join(', ') || '(No changes)'}
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
			<Typography variant="caption" color="text.secondary">Read-only · edit and push in Git</Typography>
            <IconButton onClick={() => setShowPipeline(false)} size="small"><CloseIcon /></IconButton>
          </Box>
        </DialogTitle>
        <DialogContent sx={{ p: 0 }}>
          {error && <Alert severity="error" sx={{ m: 1 }}>{error}</Alert>}
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
                <Chip label={selectedRun.rerun_task ? `Re-run: ${selectedRun.rerun_task}` : `Changed: ${selectedRun.changed?.join(', ') || 'none'}`} variant="outlined" />
                <Typography variant="caption" color="text.secondary" sx={{ ml: 'auto', alignSelf: 'center' }}>
                  {new Date(selectedRun.started_at).toLocaleString()}
                  {selectedRun.finished_at && ` → ${new Date(selectedRun.finished_at).toLocaleString()}`}
                </Typography>
              </Box>
              {selectedRun.error && <Alert severity="error" sx={{ mb: 1 }}>{selectedRun.error}</Alert>}
              <Box sx={{ position: 'relative' }}>
                <CopyButton text={selectedRun.output || ''} />
                <Box component="pre" sx={{ p: 1.5, maxHeight: 400, overflow: 'auto', bgcolor: (theme) => theme.palette.mode === 'dark' ? 'grey.900' : 'grey.200', borderRadius: 1, fontSize: 12, whiteSpace: 'pre-wrap' }}>
                  {selectedRun.output || '(No output)'}
                </Box>
              </Box>
            </>
          )}
        </DialogContent>
      </Dialog>
    </Box>
  );
}
