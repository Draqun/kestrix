## 1. DAG Node Spacing

- [x] 1.1 Add `display: flex; flexDirection: column; alignItems: center; gap: 0.5` to the outer Box `sx`
- [x] 1.2 Add `display="block"` prop to the path caption Typography
- [x] 1.3 Remove individual `mt: 0.5` from all status Typography elements (no longer needed with container gap)
- [x] 1.4 Verify build with `go build && cd ui && npm run build`

## 2. Verification

- [x] 2.1 Confirm DAG nodes render with consistent spacing in both light and dark mode
- [x] 2.2 Confirm no visual regression for nodes with or without path/trigger/status
