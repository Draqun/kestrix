import dagre from 'dagre';

export function layoutFromProject(project) {
  const g = new dagre.graphlib.Graph();
  g.setDefaultEdgeLabel(() => ({}));
  g.setGraph({ rankdir: 'LR', nodesep: 80, ranksep: 120 });

  project.Repos?.forEach(repo => {
    g.setNode(`repo:${repo.Name}`, { width: 170, height: 60 });
  });
  project.Tasks?.forEach(task => {
    g.setNode(`task:${task.Name}`, { width: 190, height: 60 });
  });

  project.Tasks?.forEach(task => {
    const trig = task.Trigger || '';
    const reposInTrigger = [...trig.matchAll(/repo\.([a-zA-Z0-9_]+)\.changed/g)].map(m => m[1]);
    reposInTrigger.forEach(rn => {
      if (project.Repos?.some(r => r.Name === rn)) {
        g.setEdge(`repo:${rn}`, `task:${task.Name}`);
      }
    });
    if (task.Execute?.Repo) {
      g.setEdge(`task:${task.Name}`, `repo:${task.Execute.Repo}`);
    }
    task.DependsOn?.forEach(depName => {
      g.setNode(`task:${depName}`, { width: 190, height: 60 });
      g.setEdge(`task:${depName}`, `task:${task.Name}`);
    });
  });

  dagre.layout(g);

  const nodes = [];
  const edges = [];

  project.Repos?.forEach(repo => {
    const pos = g.node(`repo:${repo.Name}`) || { x: 100, y: 100 };
    nodes.push({
      id: `repo:${repo.Name}`,
      position: { x: pos.x - 85, y: pos.y - 30 },
      data: { label: `📦 ${repo.Name}`, type: 'repo', path: repo.Path, kind: repo.Kind },
      type: 'custom',
    });
  });

  project.Tasks?.forEach(task => {
    const pos = g.node(`task:${task.Name}`) || { x: 200, y: 100 };
    nodes.push({
      id: `task:${task.Name}`,
      position: { x: pos.x - 95, y: pos.y - 30 },
      data: { label: `⚙️ ${task.Name}`, type: 'task', manual: task.Manual, trigger: task.Trigger, script: task.Execute?.Script },
      type: 'custom',
    });
  });

  project.Tasks?.forEach(task => {
    const trig = task.Trigger || '';
    const reposInTrigger = [...trig.matchAll(/repo\.([a-zA-Z0-9_]+)\.changed/g)].map(m => m[1]);
    reposInTrigger.forEach(rn => {
      if (project.Repos?.some(r => r.Name === rn)) {
        edges.push({
          id: `e-trig-${rn}-${task.Name}`,
          source: `repo:${rn}`,
          target: `task:${task.Name}`,
          animated: true,
          style: { stroke: '#ff9800', strokeWidth: 1.5 },
        });
      }
    });
    if (task.Execute?.Repo) {
      edges.push({
        id: `e-exec-${task.Name}-${task.Execute.Repo}`,
        source: `task:${task.Name}`,
        target: `repo:${task.Execute.Repo}`,
        animated: true,
        style: { stroke: '#2196f3', strokeWidth: 1.5 },
      });
    }
    task.DependsOn?.forEach(depName => {
      edges.push({
        id: `e-dep-${depName}-${task.Name}`,
        source: `task:${depName}`,
        target: `task:${task.Name}`,
        animated: false,
        style: { stroke: '#9c27b0', strokeWidth: 2.5 },
      });
    });
  });

  return { nodes, edges };
}
