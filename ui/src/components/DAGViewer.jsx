import { useEffect } from 'react';
import {
  ReactFlow, Background, Controls, MiniMap,
  useNodesState, useEdgesState, Handle, Position
} from 'reactflow';
import 'reactflow/dist/style.css';
import { Box, Typography } from '@mui/material';
import { formatSkipCategory } from '../lib/skipReason.js';
import { isWaitingForManualApproval } from '../lib/status.js';

// isManualWaiting reports whether a manual task is currently eligible for manual
// approval. When parent dependencies were absent from the current run (e.g. in
// partial, webhook, or single-task executions), the UI is optimistic and enables
// the action, leaving the final validation to the backend service.
export function isManualWaiting(job, pipelineDef, allJobs) {
  const jobName = job?.name || (job?.id?.startsWith('task:') ? job.id.replace('task:', '') : job?.id);
  const jobStatus = job?.status !== undefined ? job.status : job?.data?.runStatus;
  const jobNormalized = {
    name: jobName,
    status: jobStatus,
    skip_reason: job?.skip_reason ?? (job?.data?.skipCategory ? { category: job.data.skipCategory } : undefined),
  };

  const tasks = pipelineDef?.tasks || pipelineDef?.Tasks || [];
  const t = tasks.find(x => (x.name || x.Name) === jobName);
  if (!t || !isWaitingForManualApproval(jobNormalized, t)) return false;

  const deps = t.depends_on || t.DependsOn || [];
  if (deps.length === 0) return true;

  const statusMap = {};
  (allJobs || []).forEach(j => {
    const name = j.name || (j.id?.startsWith('task:') ? j.id.replace('task:', '') : j.id);
    const status = j.status !== undefined ? j.status : j.data?.runStatus;
    if (name) statusMap[name] = status;
  });

  return deps.every(d => {
    const status = statusMap[d];
    return status === 'passed' || status === undefined;
  });
}

// ReactFlow custom DAG node styling. Hex colors are used directly on the SVG canvas:
// green for passed, red for failed, orange for running/canceled, grey for pending/skipped,
// blue for manual/repo (see AGENTS.md DAG node colors convention).
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

  const bgColor = data.type === 'repo' ? '#132f4c'
    : data.runStatus === 'passed' ? '#143823'
    : data.runStatus === 'failed' ? '#3d1418'
    : data.runStatus === 'running' ? '#3d2810'
    : data.runStatus === 'manual' ? '#0f2f4a'
    : '#1e2630';

  return (
    <Box sx={{
      background: bgColor,
      border: `2px solid ${borderColor}`,
      borderRadius: 2, px: 2, py: 1, minWidth: 140, textAlign: 'center',
      position: 'relative',
      color: '#fff',
      display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 0.5,
      opacity: data.runStatus === 'skipped' || data.runStatus === 'pending' || data.runStatus === 'canceled' ? 0.5 : 1,
      boxShadow: data.runStatus === 'running' ? '0 0 12px rgba(255, 152, 0, 0.4)' : '0 2px 8px rgba(0,0,0,0.3)',
    }}>
      <Handle type="target" position={Position.Left} style={{ background: '#fff', width: 8, height: 8 }} />
      <Typography variant="body2" fontWeight={600}>{data.label}</Typography>
      {data.path && <Typography variant="caption" display="block" sx={{ color: 'rgba(255,255,255,0.7)' }}>{data.path}</Typography>}
      {data.repo && !data.path && (
        <Typography variant="caption" display="block" sx={{ color: 'rgba(255,255,255,0.5)', fontSize: 10 }}>
          repo: {data.repo}
        </Typography>
      )}
      {data.trigger && (
        <Typography variant="caption" color="warning.main" display="block" sx={{ fontSize: 10, wordBreak: 'break-word' }}>
          {data.trigger}
        </Typography>
      )}
      {data.manual && !data.runStatus && (
        <Typography variant="caption" display="block" sx={{ fontSize: 9, fontWeight: 600, color: '#42a5f5' }}>
          MANUAL
        </Typography>
      )}
      {data.runStatus && data.runStatus !== 'passed' && (
        <Typography variant="caption" display="block" sx={{ fontSize: 9, fontWeight: 700, color: borderColor }}>
          {data.runStatus.toUpperCase()}
          {data.runStatus === 'skipped' && data.skipCategory && (
            <span style={{ fontWeight: 400, opacity: 0.85, marginLeft: 3 }}>
              ({formatSkipCategory(data.skipCategory)})
            </span>
          )}
        </Typography>
      )}
      {data.runStatus === 'passed' && (
        <Typography variant="caption" display="block" sx={{ fontSize: 9, fontWeight: 700, color: '#4caf50' }}>
          PASSED
        </Typography>
      )}
      <Handle type="source" position={Position.Right} style={{ background: '#fff', width: 8, height: 8 }} />
    </Box>
  );
}

const nodeTypes = { custom: CustomNode };

export default function DAGViewer({ nodes: initialNodes, edges: initialEdges, onNodeClick, fitView = true }) {
  const [nodes, setNodes, onNodesChange] = useNodesState(initialNodes);
  const [edges, setEdges, onEdgesChange] = useEdgesState(initialEdges);

  useEffect(() => {
    setNodes(initialNodes);
    setEdges(initialEdges);
  }, [initialNodes, initialEdges]);

  return (
    <ReactFlow
      nodes={nodes}
      edges={edges}
      onNodesChange={onNodesChange}
      onEdgesChange={onEdgesChange}
      nodeTypes={nodeTypes}
      fitView={fitView}
      onNodeClick={onNodeClick}
      proOptions={{ hideAttribution: true }}
    >
      <Background color="#1e3a5f" />
      <Controls />
      <MiniMap nodeColor={(n) => n.data?.type === 'repo' ? '#2196f3' : '#4caf50'} maskColor="rgba(0,0,0,0.7)" />
    </ReactFlow>
  );
}
