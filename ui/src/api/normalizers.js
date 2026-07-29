export function normalizeProject(proj) {
  if (!proj) return proj;

  const normalized = { ...proj };

  // Top-level fields
  if (proj.name) normalized.Name = proj.name;
  if (proj.repos) normalized.Repos = proj.repos;
  if (proj.tasks) normalized.Tasks = proj.tasks;
  if (proj.default_image) normalized.DefaultImage = proj.default_image;
  if (proj.variables) normalized.Variables = proj.variables;

  // Nested repo fields
  if (normalized.Repos) {
    normalized.Repos = normalized.Repos.map(repo => {
      const r = { ...repo };
      if (repo.name) r.Name = repo.name;
      if (repo.path) r.Path = repo.path;
      if (repo.kind) r.Kind = repo.kind;
      return r;
    });
  }

  // Nested task fields
  if (normalized.Tasks) {
    normalized.Tasks = normalized.Tasks.map(task => {
      const t = { ...task };
      if (task.name) t.Name = task.name;
      if (task.trigger) t.Trigger = task.trigger;
      if (task.depends_on) t.DependsOn = task.depends_on;
      if (task.manual !== undefined) t.Manual = task.manual;
      if (task.execute) {
        t.Execute = { ...task.execute };
        if (task.execute.repo) t.Execute.Repo = task.execute.repo;
        if (task.execute.script) t.Execute.Script = task.execute.script;
        if (task.execute.image) t.Execute.Image = task.execute.image;
      }
      return t;
    });
  }

  return normalized;
}
