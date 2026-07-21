## Context

The run detail page shows job output in a `<Box component="pre">` with `overflow: auto` but no height constraint. The parent `<Box>` has `flex: 1` but is not a flex container — so the `pre` element expands with the text content and never overflows, making the scrollbar useless.

Two locations need fixing:
1. **Active job output** (line 273-278): `pre` box has no `maxHeight`, parent has `flex: 1` without `display: flex`
2. **Overall run output** (line 296-302): `pre` box has `maxHeight: 160` and `overflow: auto` — this one works but uses a fixed pixel value

## Goals / Non-Goals

**Goals:**
- Active job output fills available card height and scrolls when content overflows
- Both output containers scroll predictably

**Non-Goals:**
- No changes to the overall run output's fixed `maxHeight` — it's a summary view, 160px is fine
- No styling changes beyond height/overflow

## Decisions

### 1. Make the parent a flex container

Change the active job output's parent `<Box>` from:
```jsx
<Box sx={{ position: 'relative', flex: 1 }}>
```
to:
```jsx
<Box sx={{ position: 'relative', flex: 1, display: 'flex', flexDirection: 'column', minHeight: 0 }}>
```

This makes it a flex container, so the child `pre` with `flex: 1` fills available height. `minHeight: 0` allows shrinking below content size.

### 2. Give the `pre` element `flex: 1`

Replace the implicit content-based height with:
```jsx
<Box component="pre" sx={{ m: 0, p: 1.5, flex: 1, overflow: 'auto', ... }}>
```

This constrains the `pre` to the parent's available height. When text overflows, `overflow: auto` creates a vertical scrollbar.

### Alternative considered: fixed `maxHeight`

A fixed `maxHeight` (e.g., `maxHeight: 400`) is simpler but doesn't adapt to different viewport sizes. The flex approach fills whatever space is available in the card, which is more robust.

## Risks / Trade-offs

- [Flex chain] Adding `display: flex` inside an existing flex chain could interact unexpectedly — need to verify on both the job output and DAG views. Mitigation: the active job view and DAG view are mutually exclusive (ternary), so changes to the job output branch don't affect the DAG branch.
