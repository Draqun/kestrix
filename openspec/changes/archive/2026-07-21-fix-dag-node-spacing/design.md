## Context

The DAG node in `DAGViewer.jsx` renders a `Box` containing Typography children (label, path, trigger, status). Currently, each child has ad-hoc margins (`mt: 0.5` on status), while others have none — causing inconsistent spacing that looks cramped.

## Goals / Non-Goals

**Goals:**
- Consistent vertical spacing between all node children
- Clean layout regardless of which children are present (label only, label+status, label+trigger+status, etc.)
- Minimum diff, zero behavioral changes outside styling

**Non-Goals:**
- No layout restructuring (React Flow Handles stay as-is)
- No theme modifications or new dependencies
- No changes to node sizing, borders, or colors

## Decisions

1. **Container `gap` over individual margins** — A single `gap: 0.5` on the outer `<Box>` `sx` provides uniform spacing between all children. No per-element margin overrides needed. This also handles the non-uniform case where optional children (path, trigger) may or may not be present.

2. **`display="block"` on path caption** — Without `display="block"`, the path `<Typography>` renders inline next to the label. Converting it to block ensures it occupies its own line and participates in the flex gap.

3. **Keep existing `display="block"` on trigger/status** — These already use `display="block"` Typography prop; no change needed beyond replacing their `mt` with the container gap.

## Risks / Trade-offs

- [Overriding `alignItems`] Setting `alignItems: 'center'` on the flex container (needed for current centering) is fine since all children are full-width block elements.
- [No risk] Change is purely CSS in one file, trivially revertible.
