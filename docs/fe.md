# Frontend

The Kestrix UI is a single‑page application built with **React 19**, **MUI v9**, and **Vite 8**. It provides a graphical interface for managing remote projects, credentials, pipeline runs, and read-only pipeline definitions.

## Key Features

- **Project management** — validate, create, edit, delete, and view remote projects
- **Credential management** — create, rotate, and revoke write-only repository secrets
- **Pipeline viewer** — read-only HCL with syntax highlighting; Git is the source of truth
- **Run history** — paginated list of pipeline runs with filtering by status and repository
- **DAG visualization** — interactive task graph using React Flow
- **Documentation** — generated Markdown documentation served at `/docs/`

## Navigation and repository credentials

The responsive navigation exposes **Settings** on desktop, tablet, and the
375-pixel mobile bottom bar. `/settings` contains the application-wide
**Repository credentials** section; credentials do not have their own menu
entry.

The Projects page keeps only a compact credential selector in create and edit
dialogs. It lists public access and active credential metadata, supports
inline creation without resetting project fields, and links to Settings for
rotation and revocation. Credential type selection uses MUI `Select` and
`MenuItem`, so it works with pointer and keyboard input.

The shared credential dialog shows required/optional state and concise helper
text for every field. Focusable information controls add detailed pointer,
keyboard, touch, and assistive-technology guidance. The dialog also makes the
security boundary explicit: encrypted, write-only repository credentials are
used only for Git source acquisition and are not task or deployment secrets.

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
Frontend unit and jsdom interaction tests run through `make ui-test` from the
repository root. The target also produces an HTML report in
`ui/coverage/frontend` and enforces coverage of at least 94% statements, 89%
branches, 93% functions, and 95% lines for the changed credential, Settings,
navigation, and project-form modules. `make ui-coverage` reruns only the
measured component suite.
