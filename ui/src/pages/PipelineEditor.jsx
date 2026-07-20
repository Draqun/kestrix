import { useState, useEffect, useCallback } from 'react';
import {
  ReactFlow, Background, Controls, MiniMap,
  useNodesState, useEdgesState, MarkerType, Handle, Position
} from 'reactflow';
import 'reactflow/dist/style.css';
import {
  Box, Card, CardContent, Typography, Button, TextField, Chip,
  Alert, LinearProgress
} from '@mui/material';
import {
  PlayArrow as ResolveIcon,
  Save as SaveIcon,
  CheckCircle as TriggeredIcon,
  AccountTree as DagIcon,
} from '@mui/icons-material';
import dagre from 'dagre';
import { getProjectPipeline, saveProjectPipeline, resolveProject, parsePipeline, getProject } from '../api/client';

// Empty pipeline template for a new project
const EMPTY_TEMPLATE = `project "new-project" {
  default_image = "golang:1.26"

  ci_repo "tf_plugin" {
    path = "tf-plugin"
    kind = "subpath"
  }

  ci_repo "backend" {
    path = "backend"
    kind = "subpath"
  }

  ci_repo "frontend" {
    path = "frontend"
    kind = "subpath"
  }

  ci_task "build_tf_plugin" {
    trigger = "repo.tf_plugin.changed"
    execute {
      repo   = "tf_plugin"
      script = "make build"
    }
  }

  ci_task "build_backend" {
    trigger = "repo.backend.changed"
    execute {
      repo   = "backend"
      script = "make build"
    }
  }

  ci_task "build_frontend" {
    trigger = "repo.frontend.changed"
    execute {
      repo   = "frontend"
      script = "make build"
    }
  }
}
`;

// Build a DAG layout from the backend-parsed project model.
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
    // 1. Edge: repo → task (trigger)
    const trig = task.Trigger || '';
    const reposInTrigger = [...trig.matchAll(/repo\.([a-zA-Z0-9_]+)\.changed/g)].map(m => m[1]);
    reposInTrigger.forEach(rn => {
      if (project.Repos?.some(r => r.Name === rn)) {
        g.setEdge(`repo:${rn}`, `task:${task.Name}`);
      }
    });

    // 2. Edge: task → repo (execution destination)
    if (task.Execute?.Repo) {
      g.setEdge(`task:${task.Name}`, `repo:${task.Execute.Repo}`);
    }

    // 3. Edge: task → task (depends_on / dependencies between jobs!)
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
      data: { label: `⚙️ ${task.Name}`, type: 'task', trigger: task.Trigger, script: task.Execute?.Script },
      type: 'custom',
      style: { background: '#1a3a2a', border: '2px solid #4caf50', borderRadius: 8 },
    });
  });

  project.Tasks?.forEach(task => {
    // Edge: repo → task
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

    // Edge: task → repo
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

    // Edge: task → task (depends_on)
    task.DependsOn?.forEach(depName => {
      edges.push({
        id: `e-dep-${depName}-${task.Name}`,
        source: `task:${depName}`,
        target: `task:${task.Name}`,
        animated: false,
        markerEnd: { type: MarkerType.ArrowClosed },
        style: { stroke: '#9c27b0', strokeWidth: 2.5 }, // Purple for task-to-task dependency
      });
    });
  });

  return { nodes, edges };
}

function CustomNode({ data }) {
  return (
    <Box sx={{
      background: data.type === 'repo' ? '#1e4976' : '#1a3a2a',
      border: `2px solid ${data.type === 'repo' ? '#2196f3' : '#4caf50'}`,
      borderRadius: 2, px: 2, py: 1, minWidth: 130, textAlign: 'center',
      position: 'relative',
    }}>
      {/* Target handle on the left */}
      <Handle type="target" position={Position.Left} style={{ background: '#ffffff', width: 8, height: 8 }} />
      
      <Typography variant="body2" fontWeight={600}>{data.label}</Typography>
      {data.path && (
        <Typography variant="caption" color="text.secondary">{data.path}</Typography>
      )}
      {data.trigger && (
        <Typography variant="caption" color="warning.main" display="block" sx={{ fontSize: 10, wordBreak: 'break-word' }}>
          {data.trigger}
        </Typography>
      )}

      {/* Source handle on the right */}
      <Handle type="source" position={Position.Right} style={{ background: '#ffffff', width: 8, height: 8 }} />
    </Box>
  );
}

const nodeTypes = { custom: CustomNode };

export default function PipelineEditor({ projectName }) {
  const [hclText, setHclText] = useState(EMPTY_TEMPLATE);
  const [project, setProject] = useState(null);
  const [nodes, setNodes, onNodesChange] = useNodesState([]);
  const [edges, setEdges, onEdgesChange] = useEdgesState([]);
  const [changedRepos, setChangedRepos] = useState('');
  const [resolvedTasks, setResolvedTasks] = useState([]);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState(null);
  const [saveMsg, setSaveMsg] = useState(null);

  const loadPipeline = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      // 1. Get project config to know its local path.
      const projConfig = await getProject(projectName);

      // 2. Raw HCL from the project's repo path.
      const raw = await getProjectPipeline(projectName);
      setHclText(raw || EMPTY_TEMPLATE);

      // 3. Parsed model using backend parser pointing at repo path.
      const proj = await parsePipeline(projConfig.repo_path);
      setProject(proj);
      const { nodes: n, edges: e } = layoutFromProject(proj);
      setNodes(n);
      setEdges(e);
    } catch (e) {
      setError(e.message);
    }
    setLoading(false);
  }, [projectName, setNodes, setEdges]);

  useEffect(() => {
    if (projectName) loadPipeline();
  }, [projectName, loadPipeline]);

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
            style: {
              ...node.style,
              border: isOn ? '2px solid #4caf50' : '2px solid #f44336',
              opacity: isOn ? 1 : 0.45,
            },
          };
        }
        return node;
      }));
    } catch (e) {
      setError(e.message);
    }
  };

  if (!projectName) {
    return <Alert severity="info">Select a project from the Projects page to edit its pipeline.</Alert>;
  }

  if (loading) {
    return <Box sx={{ p: 3 }}><LinearProgress /></Box>;
  }

  return (
    <Box sx={{ height: 'calc(100vh - 100px)' }}>
      <Box sx={{ display: 'flex', alignItems: 'center', mb: 2, gap: 2 }}>
        <Typography variant="h4" sx={{ flexGrow: 0 }}>
          Pipeline: {projectName}
        </Typography>
        <Chip icon={<DagIcon />} label={`${project?.Repos?.length ?? 0} repos, ${project?.Tasks?.length ?? 0} tasks`} />
      </Box>

      <Box sx={{ display: 'flex', gap: 2, height: 'calc(100% - 60px)' }}>
        {/* HCL editor */}
        <Card sx={{ width: 440, display: 'flex', flexDirection: 'column' }}>
          <CardContent sx={{ flex: 1, display: 'flex', flexDirection: 'column', p: 1, '&:last-child': { pb: 1 } }}>
            <Typography variant="subtitle2" sx={{ mb: 1 }}>pipeline.tf</Typography>
            <TextField
              multiline
              fullWidth
              value={hclText}
              onChange={(e) => setHclText(e.target.value)}
              sx={{
                flex: 1,
                '& .MuiInputBase-root': {
                  fontFamily: 'monospace', fontSize: 13,
                  height: '100%', alignItems: 'flex-start',
                },
                '& textarea': { height: '100% !important' },
              }}
            />
            {error && <Alert severity="error" sx={{ mt: 1 }}>{error}</Alert>}
            {saveMsg && <Alert severity="success" sx={{ mt: 1 }}>{saveMsg}</Alert>}
            <Button
              sx={{ mt: 1 }}
              variant="contained"
              startIcon={<SaveIcon />}
              onClick={handleSave}
              disabled={saving}
            >
              {saving ? 'Saving…' : 'Save to repo'}
            </Button>
          </CardContent>
        </Card>

        {/* DAG Visualization */}
        <Card sx={{ flex: 1, display: 'flex', flexDirection: 'column' }}>
          <CardContent sx={{ flex: 1, display: 'flex', flexDirection: 'column', p: 1, '&:last-child': { pb: 1 } }}>
            <Box sx={{ display: 'flex', gap: 1, mb: 1, alignItems: 'center' }}>
              <TextField
                size="small"
                placeholder="Changed repos: tf_plugin, backend"
                value={changedRepos}
                onChange={(e) => setChangedRepos(e.target.value)}
                sx={{ flex: 1 }}
              />
              <Button
                variant="contained"
                startIcon={<ResolveIcon />}
                onClick={handleResolve}
                disabled={!projectName}
              >
                Resolve
              </Button>
            </Box>

            <Box sx={{ flex: 1, borderRadius: 1, overflow: 'hidden' }}>
              <ReactFlow
                nodes={nodes}
                edges={edges}
                onNodesChange={onNodesChange}
                onEdgesChange={onEdgesChange}
                nodeTypes={nodeTypes}
                fitView
                proOptions={{ hideAttribution: true }}
              >
                <Background color="#1e3a5f" />
                <Controls />
                <MiniMap
                  nodeColor={(n) => n.data?.type === 'repo' ? '#2196f3' : '#4caf50'}
                  maskColor="rgba(0,0,0,0.7)"
                />
              </ReactFlow>
            </Box>

            {resolvedTasks.length > 0 && (
              <Box sx={{ mt: 1 }}>
                <Typography variant="subtitle2" gutterBottom>Resolve Results</Typography>
                <Box sx={{ display: 'flex', gap: 1, flexWrap: 'wrap' }}>
                  {resolvedTasks.map(t => (
                    <Chip
                      key={t.name}
                      icon={<TriggeredIcon />}
                      label={t.name}
                      color="success"
                      variant="filled"
                      size="small"
                    />
                  ))}
                </Box>
              </Box>
            )}
          </CardContent>
        </Card>
      </Box>
    </Box>
  );
}
