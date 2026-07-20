// MetaCI API client
const API_BASE = import.meta.env.VITE_API_URL || '';

export async function healthCheck() {
  const res = await fetch(`${API_BASE}/api/health`);
  return res.json();
}

export async function parsePipeline(dir = './examples/demo-mono') {
  const res = await fetch(`${API_BASE}/api/pipeline?dir=${encodeURIComponent(dir)}`);
  if (!res.ok) throw new Error(`Pipeline parse failed: ${res.statusText}`);
  return res.json();
}

export async function resolveTriggers(changed = [], dir = './examples/demo-mono') {
  const res = await fetch(`${API_BASE}/api/resolve`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ changed, dir }),
  });
  if (!res.ok) throw new Error(`Resolve failed: ${res.statusText}`);
  return res.json();
}

export async function getWebhookLogs() {
  // For now, read from in-memory store
  const res = await fetch(`${API_BASE}/api/logs`);
  if (!res.ok) return [];
  return res.json();
}

export async function generateCI(dir = './examples/demo-mono', changed = []) {
  const res = await fetch(`${API_BASE}/api/generate`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ dir, changed }),
  });
  if (!res.ok) throw new Error(`Generate failed: ${res.statusText}`);
  return res.json();
}

export async function validatePipeline(dir = './examples/demo-mono') {
  const res = await fetch(`${API_BASE}/api/validate?dir=${encodeURIComponent(dir)}`);
  if (!res.ok) throw new Error(`Validate failed: ${res.statusText}`);
  return res.json();
}

// ─── Projects (UI-driven setup) ─────────────────────────────────

export async function listProjects() {
  const res = await fetch(`${API_BASE}/api/projects`);
  if (!res.ok) throw new Error(`List projects failed: ${res.statusText}`);
  return res.json();
}

export async function getProject(name) {
  const res = await fetch(`${API_BASE}/api/projects/${encodeURIComponent(name)}`);
  if (!res.ok) throw new Error(`Get project failed: ${res.statusText}`);
  return res.json();
}

export async function createProject(cfg) {
  const res = await fetch(`${API_BASE}/api/projects`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(cfg),
  });
  if (!res.ok) {
    const err = await res.json().catch(() => ({}));
    throw new Error(err.error || `Create failed: ${res.statusText}`);
  }
  return res.json();
}

export async function deleteProject(name) {
  const res = await fetch(`${API_BASE}/api/projects/${encodeURIComponent(name)}`, {
    method: 'DELETE',
  });
  if (!res.ok) {
    const err = await res.json().catch(() => ({}));
    throw new Error(err.error || `Delete failed: ${res.statusText}`);
  }
  return res.json();
}

export async function updateProject(name, cfg) {
  const res = await fetch(`${API_BASE}/api/projects/${encodeURIComponent(name)}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(cfg),
  });
  if (!res.ok) {
    const err = await res.json().catch(() => ({}));
    throw new Error(err.error || `Update failed: ${res.statusText}`);
  }
  return res.json();
}

export async function getProjectPipeline(name) {
  const res = await fetch(`${API_BASE}/api/projects/${encodeURIComponent(name)}/pipeline`);
  if (!res.ok) {
    // 404 → no pipeline file yet; return empty template.
    if (res.status === 404) return '';
    throw new Error(`Get pipeline failed: ${res.statusText}`);
  }
  return res.text();
}

export async function getProjectDefinition(name) {
  const res = await fetch(`${API_BASE}/api/projects/${encodeURIComponent(name)}/definition`);
  if (!res.ok) {
    const err = await res.json().catch(() => ({}));
    throw new Error(err.error || `Get pipeline definition failed: ${res.statusText}`);
  }
  return res.json();
}

export async function saveProjectPipeline(name, hcl) {
  const res = await fetch(`${API_BASE}/api/projects/${encodeURIComponent(name)}/pipeline`, {
    method: 'POST',
    headers: { 'Content-Type': 'text/plain' },
    body: hcl,
  });
  if (!res.ok) {
    const err = await res.json().catch(() => ({}));
    throw new Error(err.error || `Save failed: ${res.statusText}`);
  }
  return res.json();
}

export async function resolveProject(name, changed = []) {
  const res = await fetch(`${API_BASE}/api/projects/${encodeURIComponent(name)}/resolve`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ changed }),
  });
  if (!res.ok) {
    const err = await res.json().catch(() => ({}));
    throw new Error(err.error || `Resolve failed: ${res.statusText}`);
  }
  return res.json();
}

export async function runProject(name, changed = [], includeManual = false) {
  const res = await fetch(`${API_BASE}/api/projects/${encodeURIComponent(name)}/run`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ changed, include_manual: includeManual }),
  });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error(data.error || `Run failed: ${res.statusText}`);
  return data;
}

export async function listRuns(name) {
  const res = await fetch(`${API_BASE}/api/projects/${encodeURIComponent(name)}/runs`);
  if (!res.ok) return [];
  return res.json();
}

export async function getRun(name, runID) {
  const res = await fetch(`${API_BASE}/api/projects/${encodeURIComponent(name)}/runs/${runID}`);
  if (!res.ok) throw new Error(`Get run failed: ${res.statusText}`);
  return res.json();
}

export async function listAllRuns() {
  const res = await fetch(`${API_BASE}/api/runs`);
  if (!res.ok) return [];
  return res.json();
}

export async function cancelRun(name, runID) {
  const res = await fetch(`${API_BASE}/api/projects/${encodeURIComponent(name)}/runs/${runID}/cancel`, {
    method: 'POST',
  });
  if (!res.ok) {
    const err = await res.json().catch(() => ({}));
    throw new Error(err.error || `Cancel failed: ${res.statusText}`);
  }
  return res.json();
}
