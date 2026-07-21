## 1. Fix theme-aware background

- [x] 1.1 Replace `bgcolor: 'grey.900'` with `bgcolor: (theme) => theme.palette.mode === 'dark' ? 'grey.900' : 'grey.200'` in Pipelines.jsx (2 occurrences: active job output line 275, overall run output line 299)
- [x] 1.2 Replace the same in PipelineEditor.jsx (2 occurrences: run result output line 258, selected run output line 345)

## 2. Verify

- [x] 2.1 Run `npm run build` to confirm no compilation errors
- [ ] 2.2 Toggle light/dark mode on `/pipelines/runs/metaci/33` — confirm log background adapts correctly in both themes
