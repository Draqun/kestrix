export const RUN_STATUSES = ['passed', 'failed', 'running', 'canceled', 'manual', 'skipped', 'pending'];

export function filterRuns(runs, { statusFilter = [], changeFilter = '' }) {
  return runs.filter((run) => {
    if (statusFilter.length > 0 && !statusFilter.includes(run.status)) {
      return false;
    }
    if (changeFilter && !(run.changed || []).includes(changeFilter)) {
      return false;
    }
    return true;
  });
}

export function getUniqueRepos(runs) {
  const repos = new Set();
  runs.forEach((run) => {
    (run.changed || []).forEach((repo) => repos.add(repo));
  });
  return Array.from(repos).sort();
}
