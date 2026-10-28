import { useEffect } from 'react';
import {
  ReactFlow, Background, Controls, MiniMap,
  useNodesState, useEdgesState, Handle, Position
} from 'reactflow';
import 'reactflow/dist/style.css';
import { Box, Typography } from '@mui/material';

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
