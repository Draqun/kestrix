## Context

Dark palette uses harsh navy (`#0a1929` / `#132f4c`) with teal primary — feels muddy on OLED screens. App icon is a beaker (ScienceIcon), unrelated to the product name. Pagination component is a single horizontal row that overflows below 400px viewport.

## Goals / Non-Goals

**Goals:**
- Dark palette: softer, premium feel — dark slate with warm accent (amber/gold) inspired by orchestral brass.
- AppBar icon: baton/conductor or music note motif. Replace `ScienceIcon` with `MusicNote` or custom `SvgIcon`.
- Pagination: collapses to icon-only buttons on mobile; "Per page" hidden behind a compact chip.
- Keyboard nav: basic hotkey hints for power users (e.g., `g` + `d` → Dashboard).
- Copy-to-clipboard button on run output `<pre>` blocks.

**Non-Goals:**
- Not changing light palette (empty for now).
- Not adding full command palette or vim‑mode.
- Not changing backend.

## Decisions

### 1. Icon: MusicNote over custom SVG
`@mui/icons-material` has `MusicNote`, `MusicNoteOutlined`, `QueueMusic`. `MusicNote` is recognizable, scales well, and is a single import. Custom SVG would need maintenance. A baton icon isn't available in MUI defaults, but a music note is universally associated with orchestras.

### 2. Dark palette: dark slate + amber/gold
Replace navy with `#121212` (material dark) / `#1e1e2e` (slate). Primary shifts from teal to amber/gold (`#ffb300`/`amber[400]`). Secondary shifts from blueGrey to warm grey (`#a0a0b0`). AppBar gets a subtle gradient overlay.

### 3. Pagination: CSS `overflow: auto` + conditional rendering
On mobile (<600px) hide the "Per page" Select behind a simple chip popover. Use `flexWrap` and smaller button labels (icons only). The "Page X of Y" text shrinks to compact format.

### 4. Keyboard nav: useEffect with keydown listener
A single `useKeyboardNav` hook in `Layout.jsx` listens for `g` + key sequences. Implement only 3–4 shortcuts: `gd` (Dashboard), `gp` (Pipelines), `gp` (Projects), `gw` (Webhooks). Visual hints shown as a small tooltip on nav item hover.

## Risks / Trade-offs

| Risk | Mitigation |
|------|------------|
| Amber/gold primary may clash with status colors (orange for running) | Use amber only for interactive elements (buttons, links); status colors remain semantic (green/red/orange) |
| Keyboard shortcuts may conflict with browser shortcuts | Prefix all with `g` key (no standard browser shortcuts use `g` + letter) |
| Pagination layout shift on mobile | Use `flexWrap` so items naturally flow to next line instead of overflow hidden |
