export const STATUS_INFO = {
  passed: { color: 'success', label: 'PASSED' },
  failed: { color: 'error', label: 'FAILED' },
  canceled: { color: 'warning', label: 'CANCELED' },
  queued: { color: 'info', label: 'QUEUED' },
  manual: { color: 'info', label: 'MANUAL' },
  skipped: { color: 'default', label: 'SKIPPED' },
  pending: { color: 'default', label: 'PENDING' },
  running: { color: 'warning', label: 'RUNNING' },
};

const DEFAULT_STATUS = { color: 'warning', label: 'RUNNING' };

export function formatStatus(status) {
  return STATUS_INFO[status] || DEFAULT_STATUS;
}

export function getStatusColor(status) {
  return formatStatus(status).color;
}

export function isWaitingForManualApproval(job, task) {
  if (!task || !(task.manual ?? task.Manual)) return false;
  if (!job || job.skip_reason) return false;
  return job.status === 'manual';
}
