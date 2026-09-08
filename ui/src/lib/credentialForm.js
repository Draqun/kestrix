export const CREDENTIAL_TYPES = Object.freeze([
  { value: 'token', label: 'HTTPS token' },
  { value: 'basic', label: 'Username / app password' },
  { value: 'ssh', label: 'SSH private key' },
  { value: 'secret', label: 'Runtime secret' },
]);

const validTypes = new Set(CREDENTIAL_TYPES.map(item => item.value));

export const COMMON_CREDENTIAL_FIELDS = Object.freeze([
  Object.freeze({
    key: 'name',
    label: 'Display name',
    infoLabel: 'Display name',
    required: true,
    helperText: 'Required. A recognizable name shown when selecting this credential.',
    tooltip: 'Give the credential a recognizable, non-secret name. This name is shown in Settings and project credential selectors; it is not sent to the Git provider.',
  }),
  Object.freeze({
    key: 'type',
    label: 'Type',
    infoLabel: 'Type',
    required: true,
    select: true,
    helperText: 'Required. Choose the authentication method expected by the Git remote.',
    tooltip: 'Select HTTPS token, username with app password, or SSH private key. Changing the type clears entered usernames and secret fields so incompatible values cannot be submitted.',
  }),
  Object.freeze({
    key: 'provider',
    label: 'Provider (optional)',
    infoLabel: 'Provider',
    required: false,
    placeholder: 'github, gitlab, bitbucket',
    helperText: 'Optional. Searchable descriptive metadata such as GitHub, GitLab, or Bitbucket.',
    tooltip: 'This is searchable metadata only: it helps people identify the credential but does not select an authentication driver, detect a provider, validate the secret, or limit where the credential can be used.',
  }),
  Object.freeze({
    key: 'host',
    label: 'Host (optional)',
    infoLabel: 'Host',
    required: false,
    placeholder: 'git.example.com',
    helperText: 'Optional. Searchable descriptive hostname for the intended Git server.',
    tooltip: 'This hostname is searchable metadata only. It documents the intended Git server but does not validate a remote, restrict credential use, or have to match a project repository URL.',
  }),
]);

export const TYPE_CREDENTIAL_FIELDS = Object.freeze({
  token: Object.freeze([
    Object.freeze({
      key: 'username',
      label: 'HTTPS username (optional)',
      infoLabel: 'HTTPS username',
      required: false,
      placeholder: 'x-access-token',
      helperText: 'Optional. Username required by some HTTPS token providers.',
      tooltip: 'Some providers expect a fixed HTTPS username together with the token, for example x-access-token on GitHub or oauth2 on GitLab. Leave this empty when the provider accepts the token without one.',
    }),
    Object.freeze({
      key: 'token',
      label: 'Token',
      replacementLabel: 'Replacement token',
      infoLabel: 'Token',
      required: true,
      type: 'password',
      helperText: 'Required. Personal, deploy, or app token used for HTTPS Git access.',
      tooltip: 'Enter a token that can read the repository. Kestrix encrypts it at rest, uses it only while Git acquires source code, and never returns it through read APIs.',
    }),
  ]),
  basic: Object.freeze([
    Object.freeze({
      key: 'username',
      label: 'Username',
      infoLabel: 'Username',
      required: true,
      helperText: 'Required. HTTPS account or deploy-token username.',
      tooltip: 'Enter the username expected by HTTPS Git authentication. For Bitbucket this is commonly the account username paired with an app password.',
    }),
    Object.freeze({
      key: 'password',
      label: 'Password',
      replacementLabel: 'Replacement password',
      infoLabel: 'Password',
      required: true,
      type: 'password',
      helperText: 'Required. Password or app password used for HTTPS Git access.',
      tooltip: 'Enter a repository read password or app password. Kestrix encrypts it at rest, uses it only while Git acquires source code, and never returns it through read APIs.',
    }),
  ]),
  ssh: Object.freeze([
    Object.freeze({
      key: 'private_key',
      label: 'Private key',
      replacementLabel: 'Replacement private key',
      infoLabel: 'Private key',
      required: true,
      multiline: true,
      minRows: 3,
      helperText: 'Required. PEM or OpenSSH private key used for SSH Git access.',
      tooltip: 'Paste the complete private key that has read access to the repository. Kestrix encrypts it at rest, uses it only while Git acquires source code, and never returns it through read APIs.',
    }),
    Object.freeze({
      key: 'known_hosts',
      label: 'Known hosts (optional)',
      infoLabel: 'Known hosts',
      required: false,
      multiline: true,
      minRows: 2,
      placeholder: 'git.example.com ssh-ed25519 AAAA…',
      helperText: 'Optional. Recommended: pin SSH host keys to prevent server impersonation.',
      tooltip: 'Paste known_hosts entries to pin and strictly verify the Git server key. When omitted, Kestrix uses StrictHostKeyChecking=accept-new (trust on first use), which accepts the first key and rejects a later changed key.',
    }),
  ]),
  secret: Object.freeze([
    Object.freeze({
      key: 'value',
      label: 'Value',
      replacementLabel: 'Replacement value',
      infoLabel: 'Value',
      required: true,
      multiline: true,
      minRows: 3,
      helperText: 'Required. Injected into tasks as a pipeline variable; this is not a Git credential.',
      tooltip: 'Kestrix encrypts this value at rest and never returns it through read APIs. A pipeline references it by name with secret_ref and Kestrix injects the decrypted value into a task as a pipeline variable when a run starts - it is never used to authenticate Git.',
    }),
  ]),
});

export function normalizeCredentialType(value) {
  return validTypes.has(value) ? value : 'token';
}

export function credentialFieldDescriptors(type, { editing = false } = {}) {
  const normalized = normalizeCredentialType(type);
  return [...COMMON_CREDENTIAL_FIELDS, ...TYPE_CREDENTIAL_FIELDS[normalized]].map(descriptor => (
    editing && descriptor.replacementLabel
      ? { ...descriptor, label: descriptor.replacementLabel }
      : descriptor
  ));
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
    value: '',
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
    value: '',
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
  } else if (type === 'secret') {
    payload.value = form.value || '';
  } else {
    if (form.username) payload.username = form.username;
    payload.token = form.token || '';
  }
  return payload;
}

export function redactCredentialError(message, form = {}) {
  let redacted = String(message || 'Credential operation failed');
  for (const key of ['token', 'password', 'private_key', 'value']) {
    const secret = form?.[key];
    if (secret) redacted = redacted.replaceAll(secret, '[REDACTED]');
  }
  return redacted;
}
