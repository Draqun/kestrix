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
