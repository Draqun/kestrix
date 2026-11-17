import { useMemo } from 'react';
import {
  Box, Card, CardContent, Typography, Button, Chip, Alert,
  List, ListItem, ListItemText, ListItemButton,
} from '@mui/material';
import { Replay as RerunIcon } from '@mui/icons-material';
import DAGViewer from '../DAGViewer';
import CopyButton from '../CopyButton';
import SkipReasonPanel from '../SkipReasonPanel';
import { getSkipSummary } from '../../lib/skipReason';
import { formatStatus, isManualWaiting, actorLabel } from '../../lib/status.js';

const TERMINAL_RUN_STATUSES = ['passed', 'failed', 'canceled'];

const SKIPPED_SECONDARY_SX = {
  fontSize: '0.7rem',
  color: 'text.secondary',
  display: 'block',
  whiteSpace: 'nowrap',
  overflow: 'hidden',
  textOverflow: 'ellipsis',
};

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

export default function TaskLogPanel({
  run,
  loading,
  selectedJob,
  onSelectJob,
  dagNodes,
  dagEdges,
  canOperate,
  approving,
  onApprove,
  rerunning,
  onRerun,
}) {
  const r = run;
  const jobs = useMemo(() => {
    const map = {};
    (r?.task_results || []).forEach(j => { map[j.name] = j; });
    return Object.values(map);
  }, [r?.task_results]);

  const activeJob = selectedJob != null ? jobs.find(j => j.name === selectedJob) : null;
  const activeTaskDef = (r?.pipeline_def?.tasks || []).find(
    t => (t.name ?? t.Name) === activeJob?.name
  );

  return (
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
                      onClick={() => onSelectJob(selectedJob === job.name ? null : job.name)}
                      sx={{ borderRadius: 1, px: 1 }}>
                      <Chip label={js.label} color={js.color} size="small" sx={{ mr: 1, minWidth: 54 }} />
                      <ListItemText
                        primary={job.name}
                        secondary={job.status === 'skipped' ? getSkipSummary(job) : formatDuration(job.duration)}
                        slotProps={{
                          primary: { variant: 'body2', fontWeight: 600 },
                          secondary: {
                            variant: 'caption',
                            sx: job.status === 'skipped' ? SKIPPED_SECONDARY_SX : {}
                          },
                        }}
                      />
                    </ListItemButton>
                    {job.waiting_reason && (
                      <Alert severity="warning" sx={{ mt: 0.5, mx: 1, py: 0, px: 1, fontSize: '0.75rem' }}>
                        {job.waiting_reason}
                      </Alert>
                    )}
                    {canOperate && canRun && (
                      <Button size="small" variant="contained" color="info" sx={{ mt: 0.5, mx: 1 }}
                        disabled={!!approving} onClick={() => onApprove(job.name)}>
                        {approving === job.name ? 'Starting…' : 'Run'}
                      </Button>
                    )}
                    {canOperate && canRerun(job, r) && (
                      <RerunButton task={job.name} rerunning={rerunning} onRerun={onRerun} sx={{ mt: 0.5, mx: 1 }} />
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
                <Button size="small" onClick={() => onSelectJob(null)}>← DAG</Button>
                <Typography variant="subtitle1" fontWeight={700}>{activeJob.name}</Typography>
                <Chip label={formatStatus(activeJob.status).label} color={formatStatus(activeJob.status).color} size="small" />
                {activeJob.duration > 0 && (
                  <Typography variant="caption" color="text.secondary">{formatDuration(activeJob.duration)}</Typography>
                )}
                {canOperate && isManualWaiting(activeJob, r?.pipeline_def, jobs) && (
                  <Button size="small" variant="contained" color="info" disabled={!!approving}
                    onClick={() => onApprove(activeJob.name)}>
                    {approving === activeJob.name ? 'Starting…' : 'Run manual job'}
                  </Button>
                )}
                {canOperate && canRerun(activeJob, r) && (
                  <RerunButton task={activeJob.name} rerunning={rerunning} onRerun={onRerun} />
                )}
              </Box>
              {activeJob.approved_by && (
                <Typography variant="caption" color="text.secondary" display="block" sx={{ mb: 1, px: 1 }}>
                  approved by {actorLabel(activeJob.approved_by)}
                  {activeJob.approved_at && ` at ${new Date(activeJob.approved_at).toLocaleString()}`}
                </Typography>
              )}
              {activeJob.error && activeJob.status !== 'manual' && activeJob.status !== 'skipped' && (
                <Alert severity="error" sx={{ mb: 1, mx: 1 }}>{activeJob.error}</Alert>
              )}
              {activeJob.waiting_reason && (
                <Alert severity="warning" sx={{ mb: 1, mx: 1 }}>{activeJob.waiting_reason}</Alert>
              )}
              {isManualWaiting(activeJob, r?.pipeline_def, jobs) && (
                <Alert severity="info" sx={{ mb: 1, mx: 1 }}>Waiting for manual approval.</Alert>
              )}

              <SkipReasonPanel
                job={activeJob}
                taskDef={activeTaskDef}
                jobs={jobs}
                changed={r?.changed}
                onSelectJob={onSelectJob}
              />

              {(activeJob.output || activeJob.status !== 'skipped') && (
                <Box sx={{ position: 'relative', flex: 1, display: 'flex', flexDirection: 'column', minHeight: 0 }}>
                  <CopyButton text={activeJob.output || ''} />
                  <Box component="pre" sx={{ m: 0, p: 1.5, flex: 1, overflow: 'auto', bgcolor: (theme) => theme.palette.mode === 'dark' ? 'grey.900' : 'grey.200', borderRadius: 1, fontSize: 12, whiteSpace: 'pre-wrap' }}>
                    {activeJob.output || (activeJob.status === 'running' ? 'Running…' : '(No output)')}
                  </Box>
                </Box>
              )}
            </>
          ) : (
            <Box sx={{ flex: 1, borderRadius: 1, overflow: 'hidden', minHeight: 320 }}>
              {dagNodes.length > 0 ? (
                <DAGViewer nodes={dagNodes} edges={dagEdges}
                  onNodeClick={(_, node) => {
                    if (node.data?.type === 'task') onSelectJob(node.id.replace('task:', ''));
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
  );
}
