import { useCallback, useEffect, useRef, useState } from 'react';

// useListResource loads `{ items }` from fetcher and exposes it with a
// refresh. enabled lets a caller skip the initial fetch entirely - e.g. an
// admin-only Settings section never fires its admin-gated request for a
// viewer/operator session.
export default function useListResource(fetcher, enabled = true) {
  const [items, setItems] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);
  // latestRequest guards against two overlapping refresh() calls (e.g. one
  // mutation's post-success refresh still in flight when another starts):
  // only the most recently started call is allowed to apply its result.
  const latestRequest = useRef(0);

  const refresh = useCallback(async () => {
    const requestId = ++latestRequest.current;
    setLoading(true);
    setError(null);
    try {
      const response = await fetcher();
      const next = response.items || [];
      if (requestId === latestRequest.current) setItems(next);
      return next;
    } catch (cause) {
      if (requestId === latestRequest.current) setError(cause.message);
      return [];
    } finally {
      if (requestId === latestRequest.current) setLoading(false);
    }
  }, [fetcher]);

  useEffect(() => { if (enabled) refresh(); }, [enabled, refresh]);

  return { items, loading, error, setError, refresh };
}
