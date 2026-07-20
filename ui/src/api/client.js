// MetaCI API client
const API_BASE = import.meta.env.VITE_API_URL || 'http://localhost:8080';

export async function healthCheck() {
  const res = await fetch(`${API_BASE}/health`);
  return res.json();
}

export async function parsePipeline(dir = './examples') {
  const res = await fetch(`${API_BASE}/api/pipeline?dir=${encodeURIComponent(dir)}`);
  if (!res.ok) throw new Error(`Pipeline parse failed: ${res.statusText}`);
  return res.json();
}

export async function resolveTriggers(changed = [], dir = './examples') {
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

export async function generateCI(dir = './examples', changed = []) {
  const res = await fetch(`${API_BASE}/api/generate`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ dir, changed }),
  });
  if (!res.ok) throw new Error(`Generate failed: ${res.statusText}`);
  return res.json();
}

export async function validatePipeline(dir = './examples') {
  const res = await fetch(`${API_BASE}/api/validate?dir=${encodeURIComponent(dir)}`);
  if (!res.ok) throw new Error(`Validate failed: ${res.statusText}`);
  return res.json();
}
