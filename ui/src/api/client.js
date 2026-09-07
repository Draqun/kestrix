// Kestrix API client
import { normalizeProject } from './normalizers.js';

const API_BASE = import.meta.env?.VITE_API_URL || '';

export { normalizeProject };

// ─── Authentication plumbing ────────────────────────────────────
//
// AuthContext is the only caller of setAuthToken/setUnauthorizedHandler.
// Every exported request function below goes through apiFetch so a token,
// once set, is attached everywhere with nothing left to individually
// remember, and a 401 from any call is reported to the same place.

let authToken = null;
let onUnauthorized = null;

/** setAuthToken records the bearer token attached to every later request. */
export function setAuthToken(token) {
  authToken = token || null;
}

/** setUnauthorizedHandler is called once, by AuthProvider, on every 401. */
export function setUnauthorizedHandler(handler) {
  onUnauthorized = handler;
}

async function apiFetch(url, options = {}) {
  const headers = { ...(options.headers || {}) };
  if (authToken) headers['Authorization'] = `Bearer ${authToken}`;
  const res = await fetch(url, { ...options, headers });
  if (res.status === 401 && onUnauthorized) onUnauthorized();
  return res;
}

/** login exchanges credentials for a token. Never attaches a stale token. */
export async function login(name, password) {
  const res = await fetch(`${API_BASE}/api/auth/login`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name, password }),
  });
  if (!res.ok) {
    const err = await res.json().catch(() => ({}));
    throw new Error(err.error || 'Invalid name or password');
  }
  return res.json();
}

export async function healthCheck() {
  const res = await apiFetch(`${API_BASE}/api/health`);
  return res.json();
}

export async function parsePipeline(dir = './examples/demo-mono') {
  const res = await apiFetch(`${API_BASE}/api/pipeline?dir=${encodeURIComponent(dir)}`);
  if (!res.ok) throw new Error(`Pipeline parse failed: ${res.statusText}`);
  return res.json();
}

export async function resolveTriggers(changed = [], dir = './examples/demo-mono') {
  const res = await apiFetch(`${API_BASE}/api/resolve`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ changed, dir }),
  });
  if (!res.ok) throw new Error(`Resolve failed: ${res.statusText}`);
  return res.json();
}

// getWebhookLogs reads durable webhook history. Filters are optional:
// project, status, since and until (RFC3339) combine conjunctively.
export async function getWebhookLogs({
  limit = 25, offset = 0, project = '', status = '', since = '', until = '',
} = {}) {
  const params = new URLSearchParams();
  if (limit) params.append('limit', limit);
  if (offset) params.append('offset', offset);
  if (project) params.append('project', project);
  if (status) params.append('status', status);
  if (since) params.append('since', since);
  if (until) params.append('until', until);
  const query = params.toString() ? `?${params.toString()}` : '';
  const res = await apiFetch(`${API_BASE}/api/logs${query}`);
  if (!res.ok) throw new Error(`Webhook history failed: ${res.statusText}`);
  const data = await res.json();
  return {
    items: Array.isArray(data.items) ? data.items : [],
    pagination: data.pagination || { total: 0, limit, offset, next: null, prev: null },
  };
}

export async function generateCI(dir = './examples/demo-mono', changed = []) {
  const res = await apiFetch(`${API_BASE}/api/generate`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ dir, changed }),
  });
  if (!res.ok) throw new Error(`Generate failed: ${res.statusText}`);
  return res.json();
}

export async function validatePipeline(dir = './examples/demo-mono') {
  const res = await apiFetch(`${API_BASE}/api/validate?dir=${encodeURIComponent(dir)}`);
  if (!res.ok) throw new Error(`Validate failed: ${res.statusText}`);
  return res.json();
}

// ─── Projects (UI-driven setup) ─────────────────────────────────

export async function listProjects(limit = 25, offset = 0) {
  const params = new URLSearchParams();
  if (limit) params.append('limit', limit);
  if (offset) params.append('offset', offset);
  const query = params.toString() ? `?${params.toString()}` : '';
  const res = await apiFetch(`${API_BASE}/api/projects${query}`);
  if (!res.ok) throw new Error(`List projects failed: ${res.statusText}`);
  const data = await res.json();
  if (data.items && Array.isArray(data.items)) {
    data.items = data.items.map(normalizeProject);
  }
  return data;
}

export async function getProject(name) {
  const res = await apiFetch(`${API_BASE}/api/projects/${encodeURIComponent(name)}`);
  if (!res.ok) throw new Error(`Get project failed: ${res.statusText}`);
  const data = await res.json();
  return normalizeProject(data);
}

export async function createProject(cfg) {
  const res = await apiFetch(`${API_BASE}/api/projects`, {
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
  const res = await apiFetch(`${API_BASE}/api/projects/${encodeURIComponent(name)}`, {
    method: 'DELETE',
  });
  if (!res.ok) {
    const err = await res.json().catch(() => ({}));
    throw new Error(err.error || `Delete failed: ${res.statusText}`);
  }
  return res.json();
}

export async function updateProject(name, cfg) {
  const res = await apiFetch(`${API_BASE}/api/projects/${encodeURIComponent(name)}`, {
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
  const res = await apiFetch(`${API_BASE}/api/projects/${encodeURIComponent(name)}/pipeline`);
  if (!res.ok) {
    // 404 → no pipeline file yet; return empty template.
    if (res.status === 404) return '';
    throw new Error(`Get pipeline failed: ${res.statusText}`);
  }
  return res.text();
}

export async function getProjectDefinition(name) {
  const res = await apiFetch(`${API_BASE}/api/projects/${encodeURIComponent(name)}/definition`);
  if (!res.ok) {
    const err = await res.json().catch(() => ({}));
    throw new Error(err.error || `Get pipeline definition failed: ${res.statusText}`);
  }
  const data = await res.json();
  return normalizeProject(data);
}

export async function saveProjectPipeline(name, hcl) {
  const res = await apiFetch(`${API_BASE}/api/projects/${encodeURIComponent(name)}/pipeline`, {
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
  const res = await apiFetch(`${API_BASE}/api/projects/${encodeURIComponent(name)}/resolve`, {
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
  const res = await apiFetch(`${API_BASE}/api/projects/${encodeURIComponent(name)}/run`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ changed, include_manual: includeManual }),
  });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error(data.error || `Run failed: ${res.statusText}`);
  return data;
}

export async function listRuns(name, limit = 25, offset = 0) {
  const params = new URLSearchParams();
  if (limit) params.append('limit', limit);
  if (offset) params.append('offset', offset);
  const query = params.toString() ? `?${params.toString()}` : '';
  const res = await apiFetch(`${API_BASE}/api/projects/${encodeURIComponent(name)}/runs${query}`);
  if (!res.ok) return [];
  return res.json();
}

export async function getRun(name, runID) {
  const res = await apiFetch(`${API_BASE}/api/projects/${encodeURIComponent(name)}/runs/${runID}`);
  if (!res.ok) throw new Error(`Get run failed: ${res.statusText}`);
  return res.json();
}

export async function listAllRuns() {
  const res = await apiFetch(`${API_BASE}/api/runs`);
  if (!res.ok) return [];
  return res.json();
}

export async function cancelRun(name, runID) {
  const res = await apiFetch(`${API_BASE}/api/projects/${encodeURIComponent(name)}/runs/${runID}/cancel`, {
    method: 'POST',
  });
  if (!res.ok) {
    const err = await res.json().catch(() => ({}));
    throw new Error(err.error || `Cancel failed: ${res.statusText}`);
  }
  return res.json();
}

export async function getVersion() {
  const res = await apiFetch(`${API_BASE}/api/version`);
  if (!res.ok) return { version: "unknown" };
  return res.json();
}

export async function getDashboard() {
  const res = await apiFetch(`${API_BASE}/api/dashboard`);
  if (!res.ok) throw new Error(`Dashboard failed: ${res.statusText}`);
  return res.json();
}

export async function approveManualTask(name, runID, task) {
  const res = await apiFetch(
    `${API_BASE}/api/projects/${encodeURIComponent(name)}/runs/${runID}/tasks/${encodeURIComponent(task)}/run`,
    { method: 'POST' },
  );
  if (!res.ok) {
    const err = await res.json().catch(() => ({}));
    throw new Error(err.error || `Approve failed: ${res.statusText}`);
  }
  return res.json();
}

export async function listCredentials() {
  const res = await apiFetch(`${API_BASE}/api/credentials`);
  if (!res.ok) throw new Error(`List credentials failed: ${res.statusText}`);
  return res.json();
}

export async function createCredential(input) {
  const res = await apiFetch(`${API_BASE}/api/credentials`, {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(input),
  });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error(data.error || `Create credential failed: ${res.statusText}`);
  return data;
}

export async function rotateCredential(ref, input) {
  const res = await apiFetch(`${API_BASE}/api/credentials/${encodeURIComponent(ref)}`, {
    method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(input),
  });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error(data.error || `Rotate credential failed: ${res.statusText}`);
  return data;
}

export async function revokeCredential(ref) {
  const res = await apiFetch(`${API_BASE}/api/credentials/${encodeURIComponent(ref)}/revoke`, { method: 'POST' });
  if (!res.ok) throw new Error(`Revoke credential failed: ${res.statusText}`);
}
