export function remoteProjectPayload(name, form) {
  // The API keeps a stored webhook secret when the update omits the field, so
  // the key is only emitted when the user actually typed one. Sending an empty
  // string on every edit would be indistinguishable from "leave it alone" here,
  // but relying on that is what makes an accidental clear one API change away.
  const secret = (form.webhook_secret || '').trim();
  return {
    ...(name ? { name: name.trim() } : {}),
    repo_url: (form.repo_url || '').trim(),
    repo_ref: (form.repo_ref || '').trim(),
    repo_credential_ref: form.repo_credential_ref || '',
    pipeline_dir: (form.pipeline_dir || 'ci').trim(),
    pipeline_file: (form.pipeline_file || '').trim() || 'pipeline.tf',
    ...(secret ? { webhook_secret: secret } : {}),
  };
}

export function validateRemoteProject(name, form) {
  if (name !== undefined && !name.trim()) return 'Project name is required';
  if (!(form.repo_url || '').trim()) return 'Repository URL is required';
  if (!(form.repo_ref || '').trim()) return 'Repository ref is required';
  if (!(form.pipeline_dir || '').trim()) return 'Pipeline directory is required';
  return '';
}

export function activeCredentialMetadata(credentials) {
  return (credentials || []).filter(item => !item.revoked);
}

export function createdCredentialSelection(currentRef, created) {
  return created?.ref || currentRef;
}

export function repositorySourceLabel(repo) {
  if (repo.URL) return `${repo.Name}: ${repo.URL} @ ${repo.Ref}`;
  return `${repo.Name}: ${repo.Path}`;
}
