## 1. Fix active job output scroll

- [x] 1.1 Change the parent `<Box>` wrapping the job output from `{ position: 'relative', flex: 1 }` to also include `display: 'flex', flexDirection: 'column', minHeight: 0`
- [x] 1.2 Change the `<Box component="pre">` from `overflow: 'auto'` (without height constraint) to `flex: 1, overflow: 'auto'` so it fills the parent and scrolls

## 2. Verify

- [x] 2.1 Run `npm run build` to confirm no compilation errors
- [x] 2.2 Navigate to `/pipelines/runs/metaci/33` with a long-running job — confirm the output panel has a scrollbar and the logs are scrollable
