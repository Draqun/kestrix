export const CREDENTIAL_TYPES = Object.freeze([
  { value: 'token', label: 'HTTPS token' },
  { value: 'basic', label: 'Username / app password' },
  { value: 'ssh', label: 'SSH private key' },
]);

const validTypes = new Set(CREDENTIAL_TYPES.map(item => item.value));

export function normalizeCredentialType(value) {
  return validTypes.has(value) ? value : 'token';
}

export function credentialFormDefaults(metadata = {}) {
  metadata ||= {};
  return {
    name: metadata.name || '',
    type: normalizeCredentialType(metadata.type),
    provider: metadata.provider || '',
    host: metadata.host || '',
    username: '',
    password: '',
    token: '',
    private_key: '',
    known_hosts: '',
  };
}

export function changeCredentialType(form, nextType) {
  return {
    ...form,
    type: normalizeCredentialType(nextType),
    username: '',
    password: '',
    token: '',
    private_key: '',
    known_hosts: '',
  };
}

export function credentialPayload(form) {
  const type = normalizeCredentialType(form.type);
  const payload = {
    name: (form.name || '').trim(),
    type,
    provider: (form.provider || '').trim(),
    host: (form.host || '').trim(),
  };
  if (type === 'basic') {
    payload.username = form.username || '';
    payload.password = form.password || '';
  } else if (type === 'ssh') {
    payload.private_key = form.private_key || '';
    payload.known_hosts = form.known_hosts || '';
  } else {
    payload.token = form.token || '';
  }
  return payload;
}
