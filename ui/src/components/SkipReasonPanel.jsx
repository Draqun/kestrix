import { Box, Card, Typography, Chip, Alert } from '@mui/material';
import { skipSeverity, getSkipSummary } from '../lib/skipReason.js';
import { getStatusColor } from '../lib/status.js';

const cardBg = (theme) => theme.palette.mode === 'dark' ? 'grey.900' : 'grey.100';
const codeBg = (theme) => theme.palette.mode === 'dark' ? '#121212' : '#ffffff';

export default function SkipReasonPanel({ job, taskDef, jobs = [], changed = [], onSelectJob }) {
  if (!job || job.status !== 'skipped') {
    return null;
  }

  const triggerExpr = job.skip_reason?.trigger_evaluation?.expression ||
    taskDef?.trigger || taskDef?.Trigger;
  const evalTerms = job.skip_reason?.trigger_evaluation?.terms;
  const changedRepos = job.skip_reason?.trigger_evaluation?.changed_repos || changed || [];
  const dependsOn = taskDef?.depends_on ?? taskDef?.DependsOn ?? [];
  const severity = skipSeverity(job.skip_reason?.category);
  const summary = getSkipSummary(job);

  return (
    <Box sx={{ mb: 1.5, mx: 1, display: 'flex', flexDirection: 'column', gap: 1.5 }}>
      <Alert severity={severity} sx={{ py: 0.5, px: 1.5 }}>
        <Typography variant="body2" fontWeight={600}>
          {summary}
        </Typography>
        {job.skip_reason?.failed_dependencies && job.skip_reason.failed_dependencies.length > 0 && (
          <Typography variant="caption" display="block" sx={{ mt: 0.5 }}>
            Failed upstream dependencies: {job.skip_reason.failed_dependencies.join(', ')}
          </Typography>
        )}
      </Alert>

      {triggerExpr && (
        <Card variant="outlined" sx={{ p: 1.5, bgcolor: cardBg }}>
          <Typography variant="subtitle2" sx={{ mb: 1, fontWeight: 600 }}>
            Trigger Condition
          </Typography>
          <Box sx={{ mb: 1 }}>
            <Typography variant="caption" color="text.secondary" display="block">Declared trigger expression:</Typography>
            <Typography
              component="code"
              sx={{
                display: 'inline-block',
                fontFamily: 'monospace',
                fontSize: '0.85rem',
                px: 1,
                py: 0.5,
                mt: 0.5,
                borderRadius: 1,
                bgcolor: codeBg,
                border: '1px solid',
                borderColor: 'divider',
              }}
            >
              {triggerExpr}
            </Typography>
          </Box>

          <Box sx={{ mb: 1 }}>
            <Typography variant="caption" color="text.secondary" display="block">Changed repositories in this run:</Typography>
            <Box sx={{ display: 'flex', gap: 0.5, flexWrap: 'wrap', mt: 0.5 }}>
              {changedRepos.length > 0 ? (
                changedRepos.map(repo => (
                  <Chip key={repo} label={repo} size="small" variant="outlined" color="primary" sx={{ fontSize: '0.75rem', height: 22 }} />
                ))
              ) : (
                <Typography variant="caption" sx={{ fontStyle: 'italic', color: 'text.secondary' }}>None</Typography>
              )}
            </Box>
          </Box>

          {evalTerms && Object.keys(evalTerms).length > 0 && (
            <Box sx={{ mt: 1 }}>
              <Typography variant="caption" color="text.secondary" display="block" sx={{ mb: 0.5 }}>Evaluated conditions:</Typography>
              <Box sx={{ display: 'flex', flexDirection: 'column', gap: 0.5 }}>
                {Object.entries(evalTerms).map(([term, satisfied]) => (
                  <Box
                    key={term}
                    sx={{
                      display: 'flex',
                      alignItems: 'center',
                      justifyContent: 'space-between',
                      py: 0.25,
                      px: 1,
                      borderRadius: 0.5,
                      bgcolor: (theme) => theme.palette.mode === 'dark' ? 'rgba(255,255,255,0.03)' : 'rgba(0,0,0,0.02)',
                    }}
                  >
                    <Typography variant="caption" component="code" sx={{ fontFamily: 'monospace' }}>
                      {term}
                    </Typography>
                    <Chip
                      label={satisfied ? 'true' : 'false'}
                      size="small"
                      color={satisfied ? 'success' : 'default'}
                      sx={{ height: 18, fontSize: '0.7rem', fontWeight: 600 }}
                    />
                  </Box>
                ))}
              </Box>
            </Box>
          )}
        </Card>
      )}

      {dependsOn && dependsOn.length > 0 && (
        <Card variant="outlined" sx={{ p: 1.5, bgcolor: cardBg }}>
          <Typography variant="subtitle2" sx={{ mb: 1, fontWeight: 600 }}>
            Upstream Dependencies
          </Typography>
          <Box sx={{ display: 'flex', gap: 1, flexWrap: 'wrap' }}>
            {dependsOn.map(dep => {
              const depJob = jobs.find(j => j.name === dep);
              const depStatus = depJob?.status || 'pending';
              const depColor = getStatusColor(depStatus);
              return (
                <Chip
                  key={dep}
                  label={`${dep}: ${depStatus}`}
                  size="small"
                  color={depColor}
                  onClick={() => onSelectJob && onSelectJob(dep)}
                  sx={{ cursor: onSelectJob ? 'pointer' : 'default', fontSize: '0.75rem', height: 24 }}
                />
              );
            })}
          </Box>
        </Card>
      )}
    </Box>
  );
}
