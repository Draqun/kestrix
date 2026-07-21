#!/bin/bash
set -euo pipefail

echo "=== Build Documentation ==="

SOURCE_DIR="${1:-./docs}"
OUTPUT_DIR="${2:-./ui/dist/docs}"

python3 - <<'PY'
import os
import re
import markdown

source_dir = os.environ.get('SOURCE_DIR', './docs')
output_dir = os.environ.get('OUTPUT_DIR', './ui/dist/docs')

md = markdown.Markdown(extensions=['fenced_code', 'tables', 'toc'])

pages = []

for root, dirs, files in os.walk(source_dir):
    # Skip generated swagger assets and Go source
    dirs[:] = [d for d in dirs if d not in {'business-website'}]
    for name in files:
        if not name.endswith('.md'):
            continue
        src_path = os.path.join(root, name)
        rel_path = os.path.relpath(src_path, source_dir)
        # Output as <name>.html, e.g. overview.md -> overview.html
        out_rel = os.path.splitext(rel_path)[0] + '.html'
        out_path = os.path.join(output_dir, out_rel)
        os.makedirs(os.path.dirname(out_path), exist_ok=True)

        with open(src_path, 'r', encoding='utf-8') as f:
            text = f.read()

        title = os.path.splitext(name)[0].replace('-', ' ').title()
        match = re.search(r'^#\s+(.+)$', text, re.MULTILINE)
        if match:
            title = match.group(1).strip()

        html_body = md.convert(text)
        md.reset()

        html = f"""<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{title}</title>
<style>
body {{ font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif; line-height: 1.6; max-width: 900px; margin: 0 auto; padding: 2rem; color: #222; background: #fff; }}
h1, h2, h3 {{ color: #111; }}
code {{ background: #f4f4f4; padding: 0.15rem 0.3rem; border-radius: 3px; }}
pre {{ background: #f4f4f4; padding: 1rem; overflow-x: auto; border-radius: 6px; }}
a {{ color: #0366d6; }}
nav {{ margin-bottom: 1.5rem; padding-bottom: 1rem; border-bottom: 1px solid #e1e4e8; }}
</style>
</head>
<body>
<nav><a href="index.html">Docs home</a></nav>
{html_body}
</body>
</html>"""

        with open(out_path, 'w', encoding='utf-8') as f:
            f.write(html)

        pages.append((out_rel, title))

# Generate index page
index_links = '\n'.join(
    f'<li><a href="{href}">{title}</a></li>'
    for href, title in sorted(pages, key=lambda x: x[1].lower())
)
index_html = f"""<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Project Documentation</title>
<style>
body {{ font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif; line-height: 1.6; max-width: 900px; margin: 0 auto; padding: 2rem; color: #222; background: #fff; }}
a {{ color: #0366d6; }}
</style>
</head>
<body>
<h1>Project Documentation</h1>
<ul>
{index_links}
</ul>
</body>
</html>"""

index_path = os.path.join(output_dir, 'index.html')
os.makedirs(output_dir, exist_ok=True)
with open(index_path, 'w', encoding='utf-8') as f:
    f.write(index_html)

print(f"Generated {len(pages)} documentation pages in {output_dir}")
PY

echo "=== Documentation built into ${OUTPUT_DIR} ==="
