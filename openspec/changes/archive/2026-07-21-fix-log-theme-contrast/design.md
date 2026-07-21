## Context

Four log `<Box component="pre">` elements across `Pipelines.jsx` and `PipelineEditor.jsx` use `bgcolor: 'grey.900'` — a dark gray intended for the dark palette. In light mode the background stays dark while text inherits light-mode colors, producing unreadable low-contrast output.

## Goals / Non-Goals

**Goals:**
- Log background switches to a light gray (`grey.200`) in light mode
- Dark mode appearance is unchanged
- All 4 log containers are fixed in one pass

**Non-Goals:**
- No structural or layout changes
- No new MUI theme tokens

## Decisions

### 1. Use MUI sx callback for theme awareness

Replace the literal string `'grey.900'` with a callback function:
```jsx
bgcolor: (theme) => theme.palette.mode === 'dark' ? 'grey.900' : 'grey.200'
```

MUI's `sx` prop supports function values that receive the theme. This is the standard MUI pattern for theme-conditional styles. `grey.200` is a light gray that pairs well with light mode text, while `grey.900` stays unchanged for dark mode.

### Alternative considered: custom palette token

Adding a `background.code` token to the theme would be cleaner but requires changing `ColorModeContext` and the theme object — more work for the same result. The inline callback is a minimal, self-contained fix.
