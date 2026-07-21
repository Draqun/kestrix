## 1. Brand Icon & Colours

- [x] 1.1 Replace `ScienceIcon` with `MusicNoteIcon` (or `QueueMusicIcon`) in Layout.jsx AppBar
- [x] 1.2 Redesign dark palette in `theme.js`: slate background (#121212 / #1e1e2e), amber primary (#ffb300), warm grey secondary
- [x] 1.3 Add subtle gradient overlay to AppBar in dark mode
- [x] 1.4 Add copy-to-clipboard button on `<pre>` output blocks in Pipelines.jsx + PipelineEditor.jsx

## 2. Keyboard Navigation

- [x] 2.1 Create `ui/src/hooks/useKeyboardNav.js` — hook listening for `g` + key sequences
- [x] 2.2 Wire hook in Layout.jsx with shortcuts: g+d (Dashboard), g+p (Pipelines), g+r (Projects), g+w (Webhooks)
- [x] 2.3 Add keyboard shortcut tooltips to NAV_ITEMS in Layout.jsx

## 3. Responsive Pagination

- [x] 3.1 Refactor Pagination.jsx — use `useMediaQuery` to detect mobile (< 600px)
- [x] 3.2 On mobile: icon-only Previous/Next, compact "X/Y" page indicator, Per Page in a popover
- [x] 3.3 Verify pagination renders without overflow at 375px width

## 4. Verification

- [x] 4.1 Run `cd ui && npm run build` — no build errors
- [x] 4.2 Manually verify: dark mode colours render correctly
- [x] 4.3 Manually verify: music note icon shows in AppBar
- [x] 4.4 Manually verify: pagination fits mobile viewport
- [x] 4.5 Manually verify: keyboard shortcuts navigate to correct pages
