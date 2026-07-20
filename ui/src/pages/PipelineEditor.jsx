import { useState, useEffect, useCallback, useMemo } from 'react';
import {
  ReactFlow, Background, Controls, MiniMap,
  useNodesState, useEdgesState, MarkerType
} from 'reactflow';
import 'reactflow/dist/style.css';
import {
  Box, Card, CardContent, Typography, Button, TextField, Chip,
  Alert, IconButton, Tooltip, LinearProgress, ToggleButton, ToggleButtonGroup
} from '@mui/material';
import {
  Refresh as RefreshIcon,
  PlayArrow as ResolveIcon,
  CheckCircle as TriggeredIcon,
  Cancel as NotTriggeredIcon,
  AccountTree as DagIcon,
} from '@mui/icons-material';
import dagre from 'dagre';
import { parsePipeline, resolveTriggers } from '../api/client';

// Parse HCL text into a simple project structure (client-side fallback)
function parseHCLText(text) {
  const project = { repos: [], tasks: [] };
  const nameMatch = text.match(/project\s+"([^"]+)"/);
  if (nameMatch) project.name = nameMatch[1];

  // Parse repos
  const repoRegex = /ci_repo\s+"([^"]+)"\s*\{([^}]+)\}/g;
  let m;
  while ((m = repoRegex.exec(text)) !== null) {
    const attrs = {};
    m[2].trim().split('\n').forEach(line => {
      const [k, ...v] = line.split('=').map(s => s.trim());
      if (k && v.length) attrs[k] = v.join('=').replace(/"/g, '');
    });
    project.repos.push({ name: m[1], ...attrs });
  }

  // Parse tasks
  const taskRegex = /ci_task\s+"([^"]+)"\s*\{([^}]+(?:\{[^}]*\}[^}]*)*)\}/g;
  while ((m = taskRegex.exec(text)) !== null) {
    const body = m[2];
    const triggerMatch = body.match(/trigger\s*=\s*"([^"]+)"/);
    const repoMatch = body.match(/repo\s*=\s*"([^"]+)"/);
    const scriptMatch = body.match(/script\s*=\s*"([^"]+)"/);
    project.tasks.push({
      name: m[1],
      trigger: triggerMatch ? triggerMatch[1] : null,
      repo: repoMatch ? repoMatch[1] : null,
      script: scriptMatch ? scriptMatch[1] : null,
    });
  }

  return project;
}

function layoutDagre(project) {
  const g = new dagre.graphlib.Graph();
  g.setDefaultEdgeLabel(() => ({}));
  g.setGraph({ rankdir: 'LR', nodesep: 80, ranksep: 120 });

  // Add repo nodes
  project.repos.forEach(repo => {
    g.setNode(`repo:${repo.name}`, { width: 160, height: 60 });
  });

  // Add task nodes
  project.tasks.forEach(task => {
    g.setNode(`task:${task.name}`, { width: 180, height: 60 });
  });

  // Add edges: task → repo (dependency)
  project.tasks.forEach(task => {
    if (task.repo) {
      g.setEdge(`task:${task.name}`, `repo:${task.repo}`);
    }
    // Parse trigger to find dependencies
    if (task.trigger) {
      const triggerRepoMatch = task.trigger.match(/repo\.([^.]+)\.changed/);
      if (triggerRepoMatch) {
        g.setEdge(`repo:${triggerRepoMatch[1]}`, `task:${task.name}`);
      }
    }
  });

  dagre.layout(g);

  const nodes = [];
  const edges = [];

  project.repos.forEach(repo => {
    const pos = g.node(`repo:${repo.name}`);
    nodes.push({
      id: `repo:${repo.name}`,
      position: { x: pos.x - 80, y: pos.y - 30 },
      data: { label: `📦 ${repo.name}`, type: 'repo', path: repo.path, kind: repo.kind },
      type: 'custom',
      style: { background: '#1e4976', border: '2px solid #2196f3', borderRadius: 8, padding: '8px 16px' },
    });
  });

  project.tasks.forEach(task => {
    const pos = g.node(`task:${task.name}`);
    nodes.push({
      id: `task:${task.name}`,
      position: { x: pos.x - 90, y: pos.y - 30 },
      data: { label: `⚙️ ${task.name}`, type: 'task', trigger: task.trigger, script: task.script },
      type: 'custom',
      style: { background: '#1a3a2a', border: '2px solid #4caf50', borderRadius: 8, padding: '8px 16px' },
    });
  });

  project.tasks.forEach(task => {
    if (task.repo) {
      edges.push({
        id: `e-${task.name}-${task.repo}`,
        source: `task:${task.name}`,
        target: `repo:${task.repo}`,
        animated: true,
        markerEnd: { type: MarkerType.ArrowClosed },
        style: { stroke: '#2196f3' },
      });
    }
    if (task.trigger) {
      const triggerRepoMatch = task.trigger.match(/repo\.([^.]+)\.changed/);
      if (triggerRepoMatch) {
        edges.push({
          id: `e-trigger-${triggerRepoMatch[1]}-${task.name}`,
          source: `repo:${triggerRepoMatch[1]}`,
          target: `task:${task.name}`,
          animated: true,
          markerEnd: { type: MarkerType.ArrowClosed },
          style: { stroke: '#ff9800' },
        });
      }
    }
  });

  return { nodes, edges };
}

function CustomNode({ data }) {
  return (
    <Box sx={{
      background: data.type === 'repo' ? '#1e4976' : '#1a3a2a',
      border: `2px solid ${data.type === 'repo' ? '#2196f3' : '#4caf50'}`,
      borderRadius: 2, px: 2, py: 1, minWidth: 120, textAlign: 'center',
    }}>
      <Typography variant="body2" fontWeight={600}>{data.label}</Typography>
      {data.path && (
        <Typography variant="caption" color="text.secondary">{data.path}</Typography>
      )}
      {data.trigger && (
        <Typography variant="caption" color="warning.main" display="block">
          {data.trigger}
        </Typography>
      )}
    </Box>
  );
}

const nodeTypes = { custom: CustomNode };

export default function PipelineEditor() {
  const [hclText, setHclText] = useState('');
  const [project, setProject] = useState(null);
  const [nodes, setNodes, onNodesChange] = useNodesState([]);
  const [edges, setEdges, onEdgesChange] = useEdgesState([]);
  const [changedRepos, setChangedRepos] = useState('');
  const [resolvedTasks, setResolvedTasks] = useState([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState(null);

  // Load example pipeline
  useEffect(() => {
    fetch('/examples/pipeline.tf')
      .then(r => r.ok ? r.text() : Promise.reject('Cannot load example'))
      .then(text => { setHclText(text); parseText(text); })
      .catch(() => {
        // Fallback to inline example
        const example = `project "demo-mono" {
  default_image = "golang:1.26"

  variable "ENVIRONMENT" {
    value = "staging"
  }

  ci_repo "auth_service" {
    path = "src/auth"
    kind = "subpath"
  }

  ci_repo "billing_service" {
    path = "src/billing"
    kind = "subpath"
  }

  ci_repo "docs" {
    path = "docs"
  }

  ci_repo "api_gateway" {
    path = "src/api-gateway"
    kind = "subpath"
  }

  ci_task "test_auth" {
    trigger = "repo.auth_service.changed"
    execute {
      repo   = "auth_service"
      script = "make test"
    }
  }

  ci_task "test_billing" {
    trigger = "repo.billing_service.changed"
    execute {
      repo   = "billing_service"
      script = "make test"
    }
  }

  ci_task "integration_tests" {
    trigger = "repo.auth_service.changed || repo.billing_service.changed && !repo.docs.changed"
    execute {
      repo   = "api_gateway"
      script = "make test-integration"
    }
  }

  ci_task "deploy_docs" {
    trigger = "repo.docs.changed"
    execute {
      repo   = "docs"
      script = "make docs-deploy"
    }
  }
}`;
        setHclText(example);
        parseText(example);
      });
  }, []);

  const parseText = (text) => {
    try {
      const proj = parseHCLText(text);
      setProject(proj);
      const { nodes: n, edges: e } = layoutDagre(proj);
      setNodes(n);
      setEdges(e);
      setError(null);
    } catch (e) {
      setError(e.message);
    }
  };

  const handleResolve = () => {
    if (!project) return;
    const changed = changedRepos.split(',').map(s => s.trim()).filter(Boolean);
    const triggered = [];

    project.tasks.forEach(task => {
      if (!task.trigger) {
        triggered.push({ name: task.name, triggered: true, reason: 'no trigger (always runs)' });
        return;
      }

      let result = false;
      // Simple evaluation for PoC
      if (task.trigger.includes('||')) {
        const parts = task.trigger.split('||').map(p => p.trim());
        result = parts.some(part => {
          const notMatch = part.match(/!repo\.([^.]+)\.changed/);
          if (notMatch) return !changed.includes(notMatch[1]);
          const match = part.match(/repo\.([^.]+)\.changed/);
          return match ? changed.includes(match[1]) : false;
        });
      } else if (task.trigger.includes('&&')) {
        const parts = task.trigger.split('&&').map(p => p.trim());
        result = parts.every(part => {
          const notMatch = part.match(/!repo\.([^.]+)\.changed/);
          if (notMatch) return !changed.includes(notMatch[1]);
          const match = part.match(/repo\.([^.]+)\.changed/);
          return match ? changed.includes(match[1]) : false;
        });
      } else {
        const notMatch = task.trigger.match(/!repo\.([^.]+)\.changed/);
        if (notMatch) {
          result = !changed.includes(notMatch[1]);
        } else {
          const match = task.trigger.match(/repo\.([^.]+)\.changed/);
          result = match ? changed.includes(match[1]) : false;
        }
      }

      triggered.push({ name: task.name, triggered: result, reason: task.trigger });
    });

    setResolvedTasks(triggered);

    // Highlight triggered nodes
    setNodes(prev => prev.map(node => {
      const taskName = node.id.replace('task:', '');
      const taskResult = triggered.find(t => t.name === taskName);
      if (taskResult) {
        return {
          ...node,
          style: {
            ...node.style,
            border: taskResult.triggered ? '2px solid #4caf50' : '2px solid #f44336',
            opacity: taskResult.triggered ? 1 : 0.5,
          },
        };
      }
      return node;
    }));
  };

  const handleTextChange = (e) => {
    setHclText(e.target.value);
    parseText(e.target.value);
  };

  return (
    <Box sx={{ height: 'calc(100vh - 100px)' }}>
      <Box sx={{ display: 'flex', alignItems: 'center', mb: 2, gap: 2 }}>
        <Typography variant="h4" sx={{ flexGrow: 0 }}>
          Pipeline Editor
        </Typography>
        <Chip icon={<DagIcon />} label={`${project?.repos?.length ?? 0} repos, ${project?.tasks?.length ?? 0} tasks`} />
      </Box>

      <Box sx={{ display: 'flex', gap: 2, height: 'calc(100% - 60px)' }}>
        {/* HCL Editor Panel */}
        <Card sx={{ width: 400, display: 'flex', flexDirection: 'column' }}>
          <CardContent sx={{ flex: 1, display: 'flex', flexDirection: 'column', p: 1, '&:last-child': { pb: 1 } }}>
            <Typography variant="subtitle2" sx={{ mb: 1 }}>pipeline.tf</Typography>
            <TextField
              multiline
              fullWidth
              value={hclText}
              onChange={handleTextChange}
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
          </CardContent>
        </Card>

        {/* DAG Visualization */}
        <Card sx={{ flex: 1, display: 'flex', flexDirection: 'column' }}>
          <CardContent sx={{ flex: 1, display: 'flex', flexDirection: 'column', p: 1, '&:last-child': { pb: 1 } }}>
            <Box sx={{ display: 'flex', gap: 1, mb: 1, alignItems: 'center' }}>
              <TextField
                size="small"
                placeholder="Changed repos: auth_service, docs"
                value={changedRepos}
                onChange={(e) => setChangedRepos(e.target.value)}
                sx={{ flex: 1 }}
              />
              <Button
                variant="contained"
                startIcon={<ResolveIcon />}
                onClick={handleResolve}
                disabled={!project}
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

            {/* Resolve Results */}
            {resolvedTasks.length > 0 && (
              <Box sx={{ mt: 1 }}>
                <Typography variant="subtitle2" gutterBottom>Resolve Results</Typography>
                <Box sx={{ display: 'flex', gap: 1, flexWrap: 'wrap' }}>
                  {resolvedTasks.map(t => (
                    <Chip
                      key={t.name}
                      icon={t.triggered ? <TriggeredIcon /> : <NotTriggeredIcon />}
                      label={t.name}
                      color={t.triggered ? 'success' : 'error'}
                      variant={t.triggered ? 'filled' : 'outlined'}
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
