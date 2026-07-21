# Frontend

The Kestrix UI is a single‑page application built with **React 19**, **MUI v9**, and **Vite 8**. It provides a graphical interface for managing projects, viewing pipeline runs, and editing pipeline definitions.

## Key Features

- **Project management** — create, edit, delete, and view projects
- **Pipeline editor** — HCL-based pipeline file editor with syntax highlighting
- **Run history** — paginated list of pipeline runs with filtering by status and repository
- **DAG visualization** — interactive task graph using React Flow
- **Documentation** — generated Markdown documentation served at `/docs/`

## Tech Stack

| Layer | Choice |
|-------|--------|
| Framework | React 19 |
| UI library | MUI v9 (Material UI) |
| Build tool | Vite 8 |
| Linter | oxlint |
| Diagrams | React Flow + Dagre |

## Development

```bash
cd ui
npm run dev      # start dev server on :3000
npm run build    # production build → ui/dist
npm run lint     # oxlint
```

The Vite dev server proxies API requests to `http://localhost:8080`.
