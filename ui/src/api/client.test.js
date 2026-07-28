import { describe, it } from 'node:test';
import assert from 'node:assert';
import { normalizeProject } from './normalizers.js';

describe('normalizeProject', () => {
  it('returns null/undefined input unchanged', () => {
    assert.strictEqual(normalizeProject(null), null);
    assert.strictEqual(normalizeProject(undefined), undefined);
  });

  it('normalizes top-level fields from lowercase to capitalized', () => {
    const input = {
      name: 'demo',
      repos: [],
      tasks: [],
      default_image: 'golang:1.26',
      variables: [],
    };
    const result = normalizeProject(input);
    assert.strictEqual(result.Name, 'demo');
    assert.deepStrictEqual(result.Repos, []);
    assert.deepStrictEqual(result.Tasks, []);
    assert.strictEqual(result.DefaultImage, 'golang:1.26');
    assert.deepStrictEqual(result.Variables, []);
  });

  it('normalizes nested repo fields', () => {
    const input = {
      name: 'demo',
      repos: [
        { name: 'auth', path: 'src/auth', kind: 'subpath' },
        { name: 'frontend', path: 'src/frontend', kind: 'git' },
      ],
      tasks: [],
    };
    const result = normalizeProject(input);
    assert.strictEqual(result.Repos.length, 2);
    assert.strictEqual(result.Repos[0].Name, 'auth');
    assert.strictEqual(result.Repos[0].Path, 'src/auth');
    assert.strictEqual(result.Repos[0].Kind, 'subpath');
    assert.strictEqual(result.Repos[1].Name, 'frontend');
    assert.strictEqual(result.Repos[1].Path, 'src/frontend');
    assert.strictEqual(result.Repos[1].Kind, 'git');
  });

  it('normalizes nested task fields including execute block', () => {
    const input = {
      name: 'demo',
      repos: [],
      tasks: [
        {
          name: 'build',
          trigger: 'repo.auth.changed',
          depends_on: ['lint'],
          manual: true,
          execute: {
            repo: 'auth',
            script: 'make build',
            image: 'golang:1.26',
          },
        },
      ],
    };
    const result = normalizeProject(input);
    assert.strictEqual(result.Tasks.length, 1);
    const task = result.Tasks[0];
    assert.strictEqual(task.Name, 'build');
    assert.strictEqual(task.Trigger, 'repo.auth.changed');
    assert.deepStrictEqual(task.DependsOn, ['lint']);
    assert.strictEqual(task.Manual, true);
    assert.strictEqual(task.Execute.Repo, 'auth');
    assert.strictEqual(task.Execute.Script, 'make build');
    assert.strictEqual(task.Execute.Image, 'golang:1.26');
  });

  it('preserves original lowercase fields alongside normalized ones', () => {
    const input = { name: 'demo', repos: [], tasks: [] };
    const result = normalizeProject(input);
    assert.strictEqual(result.name, 'demo');
    assert.strictEqual(result.Name, 'demo');
  });

  it('handles empty project object', () => {
    const result = normalizeProject({});
    assert.strictEqual(result.Name, undefined);
    assert.strictEqual(result.Repos, undefined);
    assert.strictEqual(result.Tasks, undefined);
  });

  it('handles project with no repos or tasks arrays', () => {
    const input = { name: 'demo' };
    const result = normalizeProject(input);
    assert.strictEqual(result.Name, 'demo');
    assert.strictEqual(result.Repos, undefined);
    assert.strictEqual(result.Tasks, undefined);
  });
});
