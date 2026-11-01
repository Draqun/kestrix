export const SKIP_CATEGORIES = {
  TRIGGER_UNSATISFIED: 'trigger_unsatisfied',
  DEPENDENCY_FAILED: 'dependency_failed',
  DEPENDENCY_CANCELED: 'dependency_canceled',
  RUN_CANCELED: 'run_canceled',
};

const CATEGORY_INFO = {
  [SKIP_CATEGORIES.TRIGGER_UNSATISFIED]: {
    label: 'trigger',
    fallbackSummary: 'Task skipped: trigger condition was not satisfied',
  },
  [SKIP_CATEGORIES.DEPENDENCY_FAILED]: {
    label: 'dep failed',
    fallbackSummary: 'Task skipped: upstream dependency failed',
  },
  [SKIP_CATEGORIES.DEPENDENCY_CANCELED]: {
    label: 'dep canceled',
    fallbackSummary: 'Task skipped: upstream dependency was canceled',
  },
  [SKIP_CATEGORIES.RUN_CANCELED]: {
    label: 'canceled',
    fallbackSummary: 'Task skipped: run was canceled',
  },
};

export function formatSkipCategory(category) {
  return CATEGORY_INFO[category]?.label || category || 'skipped';
}

export function skipSeverity(category) {
  if (category === SKIP_CATEGORIES.DEPENDENCY_FAILED || category === SKIP_CATEGORIES.DEPENDENCY_CANCELED) {
    return 'warning';
  }
  return 'info';
}

export function getSkipSummary(job) {
  if (job?.skip_reason?.summary) {
    return job.skip_reason.summary;
  }
  const category = job?.skip_reason?.category;
  return CATEGORY_INFO[category]?.fallbackSummary || job?.error || 'Task skipped';
}
