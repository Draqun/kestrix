export function remoteProjectPayload(name, form) {
  return {
    ...(name ? { name: name.trim() } : {}),
    repo_url: (form.repo_url || '').trim(),
    repo_ref: (form.repo_ref || '').trim(),
    repo_credential_ref: form.repo_credential_ref || '',
    pipeline_dir: (form.pipeline_dir || 'ci').trim(),
    pipeline_file: form.pipeline_file || 'pipeline.tf',
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
