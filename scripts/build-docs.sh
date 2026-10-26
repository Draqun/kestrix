#!/bin/bash
set -euo pipefail

echo "=== Build Documentation Site ==="

SOURCE_DIR="${1:-./docs}"
OUTPUT_DIR="${2:-./ui/dist/docs}"

export SOURCE_DIR
export OUTPUT_DIR

python3 - <<'PY'
import os
import re
import html
import markdown

source_dir = os.environ.get('SOURCE_DIR', './docs')
output_dir = os.environ.get('OUTPUT_DIR', './ui/dist/docs')

os.makedirs(output_dir, exist_ok=True)

# Categories and ordering of documentation pages
CATEGORIES = [
    {
        "id": "getting-started",
        "title": "Getting Started",
        "icon": "🚀",
        "pages": [
            ("overview.md", "Project Overview", "High-level concepts, architecture overview, and feature summary."),
            ("getting-started.md", "Getting Started Guide", "Step-by-step tutorial: installation, admin setup, first pipeline, webhooks, and runners."),
            ("usage.md", "Usage Guide", "Running server locally, database profiles, concurrency bounds, and pipeline runs."),
            ("business.md", "Business Overview", "Platform value proposition, target audience, and business capabilities.")
        ]
    },
    {
        "id": "pipeline-dag",
        "title": "Pipeline Definition & DAG",
        "icon": "📐",
        "pages": [
            ("pipeline-definition.md", "Pipeline Definition (HCL)", "Complete syntax for project, ci_repo, ci_task, variables, and artifacts."),
            ("dag-engine.md", "DAG Engine & Triggers", "Dependency resolution, boolean trigger expressions, and cycle detection."),
            ("ci-generator.md", "CI Generator", "Generating vendor-compatible gitlab-ci.yml files from HCL definitions.")
        ]
    },
    {
        "id": "execution-runners",
        "title": "Execution & Runners",
        "icon": "🏃",
        "pages": [
            ("ci-tools.md", "CI Tools & Runner Modes", "Execution engines: host bash (local), hardened Docker, and remote polling."),
            ("remote-runners.md", "Remote Runners Guide", "Registering runners, worker tags, HTTPS polling, and task execution."),
            ("remote-runner-scaling.md", "Scaling Remote Runners", "High concurrency, worker pools, auto-recovery, and distributed workloads."),
            ("artifact-storage-architecture.md", "Artifact Storage Architecture", "Task artifact persistence, compression, and delivery between dependent jobs.")
        ]
    },
    {
        "id": "projects-webhooks",
        "title": "Projects & Webhooks",
        "icon": "🔌",
        "pages": [
            ("remote-projects.md", "Remote Projects & Sources", "Managing Git repositories, workspace isolation, and bare cache."),
            ("repository-credentials.md", "Repository Credentials", "Encrypted HTTPS tokens, basic auth, and SSH private keys."),
            ("webhook-receiver.md", "Webhook Receiver", "GitLab and GitHub push event payloads, HMAC signatures, and run dispatch."),
            ("terraform-provider.md", "Terraform Provider", "Managing Kestrix projects, credentials, and settings as code.")
        ]
    },
    {
        "id": "security-ops",
        "title": "Security & Operations",
        "icon": "🔐",
        "pages": [
            ("authentication.md", "Authentication & RBAC", "User accounts, password hashing, JWT sessions, and role permissions."),
            ("deployment.md", "Production Deployment", "Hardened Docker Compose setup, TLS, backup policies, and master key rotation."),
            ("migrations/file-store-to-postgresql.md", "Migrating to PostgreSQL", "Transitioning state and project configs to PostgreSQL."),
            ("migrations/remote-only-projects.md", "Migrating to Remote Sources", "Updating legacy projects to remote-only source acquisition.")
        ]
    },
    {
        "id": "reference-internals",
        "title": "Reference & Architecture",
        "icon": "📚",
        "pages": [
            ("cli-reference.md", "CLI Command Reference", "Complete reference of commands: serve, run, runner, validate, user, keys."),
            ("architecture.md", "System Architecture", "Hexagonal architecture, ports, domain model, and component layout."),
            ("be.md", "Backend Architecture", "Go server, Chi routing, PGStore, services, and execution policies."),
            ("fe.md", "Frontend Architecture", "React 19, MUI theming, DAG visualizer, and API integration.")
        ]
    }
]

# Map file to metadata
file_meta = {}
for cat in CATEGORIES:
    for filename, title, desc in cat["pages"]:
        file_meta[filename] = {"title": title, "desc": desc, "category": cat["title"], "cat_icon": cat["icon"]}

# Initialize Markdown converter
md = markdown.Markdown(extensions=['fenced_code', 'tables', 'toc'])

def convert_alerts(html_content):
    """Convert GitHub-style blockquote alerts [!NOTE], [!TIP], etc."""
    alert_types = {
        'NOTE': ('alert-note', 'ℹ️ Note'),
        'TIP': ('alert-tip', '💡 Tip'),
        'IMPORTANT': ('alert-important', '❗ Important'),
        'WARNING': ('alert-warning', '⚠️ Warning'),
        'CAUTION': ('alert-caution', '🛑 Caution')
    }
    for key, (cls, label) in alert_types.items():
        pattern = re.compile(
            rf'<blockquote>\s*<p>\s*\[!{key}\]\s*(.*?)</p>\s*</blockquote>',
            re.DOTALL | re.IGNORECASE
        )
        html_content = pattern.sub(
            rf'<div class="alert {cls}"><div class="alert-title">{label}</div><div class="alert-content"><p>\1</p></div></div>',
            html_content
        )
    return html_content

def rewrite_markdown_links(html_content, rel_depth):
    """Rewrite relative .md links to .html links."""
    def repl(match):
        href = match.group(1)
        anchor = match.group(2) or ''
        if href.startswith(('http://', 'https://', 'mailto:', '//')):
            return match.group(0)
        # e.g. usage.md -> usage.html
        return f'href="{href}.html{anchor}"'
    return re.sub(r'href="([^"#:]+)\.md(#.*?)?"', repl, html_content)

def build_sidebar(current_rel, root_prefix):
    """Build the categorized sidebar HTML."""
    sidebar_items = []
    for cat in CATEGORIES:
        links_html = []
        for filename, title, _ in cat["pages"]:
            page_rel = os.path.splitext(filename)[0] + '.html'
            href = root_prefix + page_rel
            is_active = (current_rel == page_rel)
            active_cls = ' active' if is_active else ''
            links_html.append(f'<li class="nav-item"><a href="{href}" class="nav-link{active_cls}">{html.escape(title)}</a></li>')

        sidebar_items.append(f"""
        <div class="nav-group">
            <div class="nav-group-header">
                <span class="nav-group-icon">{cat["icon"]}</span>
                <span class="nav-group-title">{html.escape(cat["title"])}</span>
            </div>
            <ul class="nav-group-links">
                {''.join(links_html)}
            </ul>
        </div>
        """)
    return ''.join(sidebar_items)

# Common CSS Stylesheet
COMMON_CSS = """
:root {
  --bg-primary: #0a1929;
  --bg-secondary: #0d2137;
  --bg-tertiary: #10243e;
  --bg-surface: #132f4c;
  --text-primary: #f0f4f8;
  --text-secondary: #90a0b0;
  --text-muted: #607080;
  --accent: #ffb300;
  --accent-hover: #ffa000;
  --border-color: rgba(255, 255, 255, 0.08);
  --border-subtle: rgba(255, 255, 255, 0.04);
  --sidebar-width: 290px;
  --header-height: 64px;
  --radius-sm: 6px;
  --radius-md: 10px;
  --radius-lg: 16px;
}

* { box-sizing: border-box; margin: 0; padding: 0; }
body {
  font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Oxygen, Ubuntu, Cantarell, sans-serif;
  background: var(--bg-primary);
  color: var(--text-primary);
  line-height: 1.65;
  font-size: 15px;
}

/* Header */
.top-header {
  position: fixed;
  top: 0;
  left: 0;
  right: 0;
  height: var(--header-height);
  background: rgba(10, 25, 41, 0.92);
  backdrop-filter: blur(12px);
  border-bottom: 1px solid var(--border-color);
  z-index: 1000;
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 1.5rem;
}
.brand-wrapper {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  text-decoration: none;
}
.brand-logo {
  font-size: 1.35rem;
  font-weight: 800;
  color: #ffffff;
  letter-spacing: -0.5px;
}
.brand-logo span { color: var(--accent); }
.brand-badge {
  background: rgba(255, 179, 0, 0.15);
  color: var(--accent);
  border: 1px solid rgba(255, 179, 0, 0.3);
  font-size: 0.75rem;
  font-weight: 600;
  padding: 0.15rem 0.5rem;
  border-radius: 12px;
}
.search-wrapper {
  position: relative;
  flex: 1;
  max-width: 440px;
  margin: 0 1.5rem;
}
.search-input {
  width: 100%;
  background: rgba(255, 255, 255, 0.05);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  padding: 0.45rem 0.75rem 0.45rem 2.2rem;
  color: var(--text-primary);
  font-size: 0.88rem;
  outline: none;
  transition: border-color 0.2s, background 0.2s;
}
.search-input:focus {
  border-color: var(--accent);
  background: rgba(255, 255, 255, 0.08);
}
.search-icon {
  position: absolute;
  left: 0.75rem;
  top: 50%;
  transform: translateY(-50%);
  color: var(--text-muted);
  font-size: 0.85rem;
  pointer-events: none;
}
.header-links {
  display: flex;
  align-items: center;
  gap: 1.25rem;
}
.header-link {
  color: var(--text-secondary);
  text-decoration: none;
  font-size: 0.88rem;
  font-weight: 500;
  transition: color 0.2s;
}
.header-link:hover { color: var(--accent); }
.header-btn {
  background: var(--accent);
  color: #0a1929;
  font-weight: 700;
  padding: 0.45rem 1rem;
  border-radius: var(--radius-sm);
  text-decoration: none;
  font-size: 0.85rem;
  transition: background 0.2s, transform 0.15s;
}
.header-btn:hover { background: var(--accent-hover); transform: translateY(-1px); }
.mobile-toggle {
  display: none;
  background: none;
  border: none;
  color: var(--text-primary);
  font-size: 1.5rem;
  cursor: pointer;
}

/* Layout */
.app-layout {
  display: flex;
  margin-top: var(--header-height);
  min-height: calc(100vh - var(--header-height));
}
.sidebar {
  width: var(--sidebar-width);
  background: #081422;
  border-right: 1px solid var(--border-color);
  position: fixed;
  top: var(--header-height);
  bottom: 0;
  left: 0;
  overflow-y: auto;
  padding: 1.25rem 0.75rem 2rem 0.75rem;
  scrollbar-width: thin;
  scrollbar-color: var(--border-color) transparent;
}
.nav-group { margin-bottom: 1.25rem; }
.nav-group-header {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  padding: 0.4rem 0.6rem;
  font-size: 0.78rem;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  color: var(--text-muted);
}
.nav-group-icon { font-size: 0.95rem; }
.nav-group-links { list-style: none; margin-top: 0.25rem; }
.nav-item { margin-bottom: 0.15rem; }
.nav-link {
  display: block;
  padding: 0.45rem 0.75rem;
  color: var(--text-secondary);
  text-decoration: none;
  font-size: 0.88rem;
  border-radius: var(--radius-sm);
  transition: all 0.15s;
}
.nav-link:hover {
  color: #fff;
  background: rgba(255, 255, 255, 0.04);
}
.nav-link.active {
  color: var(--accent);
  background: rgba(255, 179, 0, 0.08);
  font-weight: 600;
  border-left: 3px solid var(--accent);
}

.main-content {
  margin-left: var(--sidebar-width);
  flex: 1;
  max-width: 980px;
  padding: 2.5rem 3.5rem 4rem 3.5rem;
}

/* Breadcrumbs */
.breadcrumbs {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  font-size: 0.82rem;
  color: var(--text-muted);
  margin-bottom: 1.5rem;
}
.breadcrumbs a { color: var(--text-secondary); text-decoration: none; }
.breadcrumbs a:hover { color: var(--accent); }
.breadcrumbs span { color: var(--border-color); }

/* Typography & Content */
h1, h2, h3, h4, h5, h6 {
  color: #ffffff;
  font-weight: 700;
  letter-spacing: -0.3px;
}
h1 { font-size: 2.2rem; margin-bottom: 1.25rem; line-height: 1.25; }
h2 { font-size: 1.55rem; margin-top: 2.5rem; margin-bottom: 1rem; padding-bottom: 0.5rem; border-bottom: 1px solid var(--border-color); }
h3 { font-size: 1.25rem; margin-top: 2rem; margin-bottom: 0.75rem; }
h4 { font-size: 1.05rem; margin-top: 1.5rem; margin-bottom: 0.5rem; }
p, ul, ol { margin-bottom: 1.2rem; }
ul, ol { padding-left: 1.75rem; }
li { margin-bottom: 0.35rem; }
a { color: var(--accent); text-decoration: none; }
a:hover { text-decoration: underline; }
hr { border: none; border-top: 1px solid var(--border-color); margin: 2.5rem 0; }

code {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", "Courier New", monospace;
  font-size: 0.88em;
  background: rgba(255, 255, 255, 0.06);
  padding: 0.2rem 0.4rem;
  border-radius: 4px;
  color: #ffe082;
}

pre {
  position: relative;
  background: #06101c;
  border: 1px solid var(--border-color);
  border-radius: var(--radius-md);
  padding: 1.25rem;
  overflow-x: auto;
  margin-bottom: 1.5rem;
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.25);
}
pre code {
  background: transparent;
  padding: 0;
  color: #e6edf3;
  font-size: 0.88rem;
  line-height: 1.6;
}

/* Tables */
table {
  width: 100%;
  border-collapse: collapse;
  margin-bottom: 1.75rem;
  font-size: 0.9rem;
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  overflow: hidden;
}
th, td {
  padding: 0.75rem 1rem;
  text-align: left;
  border-bottom: 1px solid var(--border-color);
}
th {
  background: var(--bg-tertiary);
  color: #ffffff;
  font-weight: 600;
}
tr:nth-child(even) td { background: rgba(255, 255, 255, 0.02); }
tr:hover td { background: rgba(255, 255, 255, 0.04); }

/* Alerts / Callouts */
.alert {
  padding: 1rem 1.25rem;
  border-radius: var(--radius-md);
  margin-bottom: 1.5rem;
  border-left: 4px solid;
  background: var(--bg-tertiary);
}
.alert-note { border-left-color: #2196f3; background: rgba(33, 150, 243, 0.08); }
.alert-tip { border-left-color: #4caf50; background: rgba(76, 175, 80, 0.08); }
.alert-important { border-left-color: #9c27b0; background: rgba(156, 39, 176, 0.08); }
.alert-warning { border-left-color: #ff9800; background: rgba(255, 152, 0, 0.08); }
.alert-caution { border-left-color: #f44336; background: rgba(244, 67, 54, 0.08); }
.alert-title { font-weight: 700; margin-bottom: 0.4rem; font-size: 0.92rem; }
.alert-content p { margin-bottom: 0; }

/* Copy button */
.copy-btn {
  position: absolute;
  top: 0.6rem;
  right: 0.6rem;
  background: rgba(255, 255, 255, 0.08);
  border: 1px solid var(--border-color);
  color: var(--text-secondary);
  padding: 0.25rem 0.55rem;
  border-radius: 4px;
  font-size: 0.75rem;
  cursor: pointer;
  transition: all 0.2s;
}
.copy-btn:hover { background: var(--accent); color: #0a1929; font-weight: 600; }

/* Page Footer Navigation */
.page-nav-footer {
  display: flex;
  justify-content: space-between;
  margin-top: 3.5rem;
  padding-top: 1.5rem;
  border-top: 1px solid var(--border-color);
}
.page-nav-card {
  text-decoration: none;
  background: var(--bg-tertiary);
  border: 1px solid var(--border-color);
  padding: 1rem 1.25rem;
  border-radius: var(--radius-md);
  max-width: 48%;
  flex: 1;
  transition: border-color 0.2s, transform 0.15s;
}
.page-nav-card:hover {
  border-color: var(--accent);
  transform: translateY(-2px);
  text-decoration: none;
}
.page-nav-label { font-size: 0.75rem; color: var(--text-muted); text-transform: uppercase; font-weight: 600; margin-bottom: 0.25rem; }
.page-nav-title { font-size: 1rem; font-weight: 600; color: #fff; }

/* Index Hub Cards */
.hero-hub {
  padding: 2.5rem 0 3rem 0;
  border-bottom: 1px solid var(--border-color);
  margin-bottom: 2.5rem;
}
.hero-hub h1 { font-size: 2.75rem; margin-bottom: 1rem; }
.hero-hub p { font-size: 1.2rem; color: var(--text-secondary); max-width: 720px; line-height: 1.6; margin-bottom: 1.75rem; }
.hero-hub-actions { display: flex; gap: 1rem; align-items: center; }
.btn-primary {
  background: var(--accent);
  color: #0a1929;
  font-weight: 700;
  padding: 0.75rem 1.5rem;
  border-radius: var(--radius-sm);
  text-decoration: none;
  transition: all 0.2s;
  display: inline-flex;
  align-items: center;
  gap: 0.5rem;
}
.btn-primary:hover { background: var(--accent-hover); transform: translateY(-2px); text-decoration: none; }
.btn-outline {
  background: transparent;
  color: #fff;
  border: 1px solid var(--border-color);
  font-weight: 600;
  padding: 0.75rem 1.5rem;
  border-radius: var(--radius-sm);
  text-decoration: none;
  transition: all 0.2s;
}
.btn-outline:hover { border-color: var(--accent); color: var(--accent); text-decoration: none; }

.cards-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
  gap: 1.5rem;
  margin-bottom: 3.5rem;
}
.card-item {
  background: var(--bg-tertiary);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-md);
  padding: 1.5rem;
  text-decoration: none;
  transition: border-color 0.2s, transform 0.2s, box-shadow 0.2s;
  display: flex;
  flex-direction: column;
}
.card-item:hover {
  border-color: var(--accent);
  transform: translateY(-3px);
  box-shadow: 0 8px 24px rgba(0, 0, 0, 0.3);
  text-decoration: none;
}
.card-icon { font-size: 2rem; margin-bottom: 0.75rem; }
.card-title { font-size: 1.15rem; font-weight: 700; color: #fff; margin-bottom: 0.5rem; }
.card-desc { font-size: 0.88rem; color: var(--text-secondary); line-height: 1.5; flex: 1; }

.toc-section { margin-bottom: 3rem; }
.toc-section-header {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  font-size: 1.35rem;
  color: #fff;
  margin-bottom: 1.25rem;
  padding-bottom: 0.5rem;
  border-bottom: 1px solid var(--border-color);
}
.toc-list { list-style: none; padding-left: 0; }
.toc-item {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  padding: 0.75rem 0.5rem;
  border-bottom: 1px solid var(--border-subtle);
}
.toc-item a { font-weight: 600; font-size: 0.95rem; }
.toc-item span { font-size: 0.85rem; color: var(--text-muted); }

/* Responsive */
@media (max-width: 900px) {
  .sidebar {
    transform: translateX(-100%);
    transition: transform 0.25s ease;
    z-index: 999;
  }
  .sidebar.open { transform: translateX(0); }
  .main-content { margin-left: 0; padding: 2rem 1.25rem; }
  .mobile-toggle { display: block; }
  .search-wrapper { display: none; }
}
"""

# Common JS for Copy Button, Search, Mobile Toggle
COMMON_JS = """
document.addEventListener('DOMContentLoaded', () => {
  // Mobile drawer toggle
  const toggle = document.querySelector('.mobile-toggle');
  const sidebar = document.querySelector('.sidebar');
  if (toggle && sidebar) {
    toggle.addEventListener('click', () => {
      sidebar.classList.toggle('open');
    });
  }

  // Copy code blocks
  document.querySelectorAll('pre').forEach(pre => {
    const btn = document.createElement('button');
    btn.className = 'copy-btn';
    btn.innerText = 'Copy';
    pre.appendChild(btn);

    btn.addEventListener('click', async () => {
      const code = pre.querySelector('code');
      const text = code ? code.innerText : pre.innerText;
      try {
        await navigator.clipboard.writeText(text);
        btn.innerText = 'Copied!';
        setTimeout(() => btn.innerText = 'Copy', 2000);
      } catch (err) {
        btn.innerText = 'Failed';
      }
    });
  });

  // Client-side quick filter
  const searchInput = document.getElementById('docs-search');
  if (searchInput) {
    searchInput.addEventListener('input', (e) => {
      const q = e.target.value.toLowerCase().trim();
      document.querySelectorAll('.nav-group').forEach(group => {
        let hasVisible = false;
        group.querySelectorAll('.nav-item').forEach(item => {
          const match = item.innerText.toLowerCase().includes(q);
          item.style.display = match ? '' : 'none';
          if (match) hasVisible = true;
        });
        group.style.display = hasVisible ? '' : 'none';
      });
    });
  }
});
"""

# Write style.css and script.js
with open(os.path.join(output_dir, 'style.css'), 'w', encoding='utf-8') as f:
    f.write(COMMON_CSS)

with open(os.path.join(output_dir, 'script.js'), 'w', encoding='utf-8') as f:
    f.write(COMMON_JS)

pages = []
ordered_pages = []

# Collect list of all pages
for cat in CATEGORIES:
    for filename, title, desc in cat["pages"]:
        ordered_pages.append((filename, title, cat["title"]))

# Process each markdown file in docs
for root, dirs, files in os.walk(source_dir):
    dirs[:] = [d for d in dirs if d not in {'business-website', '.git'}]
    for name in files:
        if not name.endswith('.md'):
            continue
        src_path = os.path.join(root, name)
        rel_path = os.path.relpath(src_path, source_dir)
        out_rel = os.path.splitext(rel_path)[0] + '.html'
        out_path = os.path.join(output_dir, out_rel)
        os.makedirs(os.path.dirname(out_path), exist_ok=True)

        depth = rel_path.count(os.sep)
        root_prefix = '../' * depth

        with open(src_path, 'r', encoding='utf-8') as f:
            text = f.read()

        title = os.path.splitext(name)[0].replace('-', ' ').title()
        match = re.search(r'^#\s+(.+)$', text, re.MULTILINE)
        if match:
            title = match.group(1).strip()

        meta = file_meta.get(rel_path, {})
        category_title = meta.get("category", "Documentation")

        html_body = md.convert(text)
        md.reset()

        html_body = convert_alerts(html_body)
        html_body = rewrite_markdown_links(html_body, depth)

        # Previous and Next buttons
        current_idx = -1
        for idx, (f_name, _, _) in enumerate(ordered_pages):
            if f_name == rel_path:
                current_idx = idx
                break

        prev_nav = ""
        next_nav = ""
        if current_idx > 0:
            p_file, p_title, _ = ordered_pages[current_idx - 1]
            p_rel = os.path.splitext(p_file)[0] + '.html'
            prev_nav = f'<a href="{root_prefix}{p_rel}" class="page-nav-card"><div class="page-nav-label">← Previous</div><div class="page-nav-title">{html.escape(p_title)}</div></a>'

        if current_idx >= 0 and current_idx < len(ordered_pages) - 1:
            n_file, n_title, _ = ordered_pages[current_idx + 1]
            n_rel = os.path.splitext(n_file)[0] + '.html'
            next_nav = f'<a href="{root_prefix}{n_rel}" class="page-nav-card" style="margin-left:auto; text-align:right;"><div class="page-nav-label">Next →</div><div class="page-nav-title">{html.escape(n_title)}</div></a>'

        nav_footer = f'<div class="page-nav-footer">{prev_nav}{next_nav}</div>' if (prev_nav or next_nav) else ''

        sidebar_html = build_sidebar(out_rel, root_prefix)

        page_html = f"""<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{html.escape(title)} — Kestrix Docs</title>
  <link rel="stylesheet" href="{root_prefix}style.css">
</head>
<body>
  <header class="top-header">
    <div style="display:flex; align-items:center;">
      <button class="mobile-toggle" aria-label="Toggle navigation">☰</button>
      <a href="{root_prefix}index.html" class="brand-wrapper">
        <span class="brand-logo">Kestrix<span>.</span></span>
        <span class="brand-badge">Docs · v0.3.0</span>
      </a>
    </div>

    <div class="search-wrapper">
      <span class="search-icon">🔍</span>
      <input type="text" id="docs-search" class="search-input" placeholder="Search documentation...">
    </div>

    <div class="header-links">
      <a href="{root_prefix}getting-started.html" class="header-link">Quick Start</a>
      <a href="{root_prefix}cli-reference.html" class="header-link">CLI Reference</a>
      <a href="{root_prefix}pipeline-definition.html" class="header-link">Pipeline Syntax</a>
      <a href="https://gitlab.com/kestrix/kestrix" target="_blank" rel="noopener" class="header-btn">GitLab</a>
    </div>
  </header>

  <div class="app-layout">
    <aside class="sidebar">
      {sidebar_html}
    </aside>

    <main class="main-content">
      <div class="breadcrumbs">
        <a href="{root_prefix}index.html">Docs</a>
        <span>/</span>
        <span>{html.escape(category_title)}</span>
        <span>/</span>
        <span>{html.escape(title)}</span>
      </div>

      <article class="doc-body">
        {html_body}
      </article>

      {nav_footer}
    </main>
  </div>

  <script src="{root_prefix}script.js"></script>
</body>
</html>"""

        with open(out_path, 'w', encoding='utf-8') as f:
            f.write(page_html)

        pages.append((out_rel, title, meta.get("desc", "")))

# Generate Hub index.html
hub_sidebar = build_sidebar('index.html', '')

# Build categorized TOC for the Hub page
hub_sections = []
for cat in CATEGORIES:
    items_html = []
    for filename, title, desc in cat["pages"]:
        rel_html = os.path.splitext(filename)[0] + '.html'
        items_html.append(f"""
        <li class="toc-item">
          <div>
            <a href="{rel_html}">{html.escape(title)}</a>
            <div style="font-size:0.85rem; color:var(--text-secondary); margin-top:0.2rem;">{html.escape(desc)}</div>
          </div>
          <span>→</span>
        </li>
        """)

    hub_sections.append(f"""
    <div class="toc-section">
      <h2 class="toc-section-header">
        <span>{cat["icon"]}</span>
        <span>{html.escape(cat["title"])}</span>
      </h2>
      <ul class="toc-list">
        {''.join(items_html)}
      </ul>
    </div>
    """)

index_html = f"""<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Kestrix Documentation — CI/CD Control Plane</title>
  <link rel="stylesheet" href="style.css">
</head>
<body>
  <header class="top-header">
    <div style="display:flex; align-items:center;">
      <button class="mobile-toggle" aria-label="Toggle navigation">☰</button>
      <a href="index.html" class="brand-wrapper">
        <span class="brand-logo">Kestrix<span>.</span></span>
        <span class="brand-badge">Docs · v0.3.0</span>
      </a>
    </div>

    <div class="search-wrapper">
      <span class="search-icon">🔍</span>
      <input type="text" id="docs-search" class="search-input" placeholder="Search documentation...">
    </div>

    <div class="header-links">
      <a href="getting-started.html" class="header-link">Quick Start</a>
      <a href="cli-reference.html" class="header-link">CLI Reference</a>
      <a href="pipeline-definition.html" class="header-link">Pipeline Syntax</a>
      <a href="https://gitlab.com/kestrix/kestrix" target="_blank" rel="noopener" class="header-btn">GitLab</a>
    </div>
  </header>

  <div class="app-layout">
    <aside class="sidebar">
      {hub_sidebar}
    </aside>

    <main class="main-content">
      <section class="hero-hub">
        <h1>CI/CD <span>Control Plane</span> Documentation</h1>
        <p>Kestrix is a repository-agnostic CI/CD control plane. Define multi-repository pipelines in declarative HCL, resolve execution graphs via smart boolean trigger logic, and run jobs locally, in hardened Docker, or across distributed remote runners.</p>
        <div class="hero-hub-actions">
          <a href="getting-started.html" class="btn-primary">⚡ 5-Minute Quick Start →</a>
          <a href="cli-reference.html" class="btn-outline">💻 CLI Reference</a>
          <a href="overview.html" class="btn-outline">📖 Architecture Overview</a>
        </div>
      </section>

      <section>
        <h2 style="margin-top:0;">Key Guides & Resources</h2>
        <div class="cards-grid">
          <a href="getting-started.html" class="card-item">
            <div class="card-icon">🚀</div>
            <div class="card-title">Getting Started Guide</div>
            <div class="card-desc">Complete walkthrough from zero: Docker installation, initial admin creation, private Git credentials, and running your first pipeline.</div>
          </a>

          <a href="pipeline-definition.html" class="card-item">
            <div class="card-icon">📐</div>
            <div class="card-title">Pipeline HCL Definition</div>
            <div class="card-desc">Declarative syntax for repositories (ci_repo), tasks (ci_task), boolean trigger expressions, variable interpolation, and manual gates.</div>
          </a>

          <a href="remote-runners.html" class="card-item">
            <div class="card-icon">🏃</div>
            <div class="card-title">Remote Runners</div>
            <div class="card-desc">Deploy kestrix runner on worker machines using outbound HTTPS long-polling. No open ports or public inbound addresses required.</div>
          </a>

          <a href="webhook-receiver.html" class="card-item">
            <div class="card-icon">🔌</div>
            <div class="card-title">Webhook Automation</div>
            <div class="card-desc">Connect GitLab and GitHub push webhooks with HMAC signatures. Automatic changed-file classification and run dispatch.</div>
          </a>

          <a href="authentication.html" class="card-item">
            <div class="card-icon">🔐</div>
            <div class="card-title">Security & RBAC</div>
            <div class="card-desc">User accounts, role-based access control (Admin, Operator, Viewer), JWT session management, and master key rotation.</div>
          </a>

          <a href="cli-reference.html" class="card-item">
            <div class="card-icon">💻</div>
            <div class="card-title">CLI Command Reference</div>
            <div class="card-desc">Comprehensive manual for the single binary: serve, run, resolve, validate, runner, user, and keys subcommands.</div>
          </a>
        </div>
      </section>

      <section>
        {''.join(hub_sections)}
      </section>
    </main>
  </div>

  <script src="script.js"></script>
</body>
</html>"""

with open(os.path.join(output_dir, 'index.html'), 'w', encoding='utf-8') as f:
    f.write(index_html)

print(f"Generated {len(pages)} documentation pages into {output_dir}")
PY

echo "=== Documentation built successfully into ${OUTPUT_DIR} ==="
