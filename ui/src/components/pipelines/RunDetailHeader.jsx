import { Box, Button, Typography, Chip, LinearProgress, Alert } from '@mui/material';
import { ArrowBack as BackIcon } from '@mui/icons-material';
import { formatStatus, actorLabel } from '../../lib/status.js';

export default function RunDetailHeader({
  projectName,
  runId,
  run,
  loading,
  error,
  approveErr,
  rerunErr,
  canceling,
  canOperate,
  onBack,
  onCancel,
  onClearApproveErr,
  onClearRerunErr,
}) {
  const r = run;
  const st = r ? formatStatus(r.status) : null;

  return (
    <>
      <Box sx={{ display: 'flex', alignItems: 'center', mb: 2, gap: 1, flexWrap: 'wrap' }}>
        <Button size="small" startIcon={<BackIcon />} onClick={onBack}>Back to runs</Button>
        <Typography variant="h4">Run #{runId}</Typography>
        <Typography variant="h6" color="text.secondary">{projectName}</Typography>
        {st && <Chip label={st.label} color={st.color} size="small" />}
        {r?.rerun_task ? (
          <Chip label={`Re-run: ${r.rerun_task}`} size="small" variant="outlined" />
        ) : r?.changed?.length > 0 && (
          <Chip label={`Changed: ${r.changed.join(', ')}`} size="small" variant="outlined" />
        )}
        <Box sx={{ flexGrow: 1 }} />
        {canOperate && (r?.status === 'running' || r?.status === 'queued') && (
          <Button variant="outlined" color="warning" size="small" onClick={onCancel} disabled={canceling}>
            {canceling ? 'Canceling…' : 'Cancel'}
          </Button>
        )}
      </Box>

      {loading && !r && <LinearProgress sx={{ mb: 2 }} />}
      {error && <Alert severity="error" sx={{ mb: 1 }}>{error}</Alert>}
      {approveErr && <Alert severity="error" sx={{ mb: 1 }} onClose={onClearApproveErr}>{approveErr}</Alert>}
      {rerunErr && <Alert severity="error" sx={{ mb: 1 }} onClose={onClearRerunErr}>{rerunErr}</Alert>}

      {r && (
        <Typography variant="caption" color="text.secondary" display="block" sx={{ mb: 1 }}>
          {new Date(r.started_at).toLocaleString()} by {actorLabel(r.started_by)}
          {r.finished_at && ` → ${new Date(r.finished_at).toLocaleString()}`}
          {r.status === 'canceled' && ` · canceled by ${actorLabel(r.canceled_by)}`}
        </Typography>
      )}

      {r?.sources?.length > 0 && (
        <Box sx={{ display: 'flex', gap: 1, flexWrap: 'wrap', mb: 1 }}>
          {r.sources.map((source, index) => (
            <Chip
              key={`${source.repo_name}-${source.resolved_revision}-${index}`}
              size="small"
              variant="outlined"
              label={`${source.repo_name}${source.primary ? ' (primary)' : ''}: ${source.url} @ ${source.requested_ref} → ${source.resolved_revision}`}
            />
          ))}
        </Box>
      )}
    </>
  );
}
