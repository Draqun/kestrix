import { useCallback, useEffect, useMemo, useState } from 'react';
import { listCredentials } from '../api/client.js';
import { activeCredentialMetadata } from '../lib/projectForm.js';

export default function useCredentials() {
  const [items, setItems] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);

  const refresh = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const response = await listCredentials();
      const next = response.items || [];
      setItems(next);
      return next;
    } catch (cause) {
      setError(cause.message);
      return [];
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { refresh(); }, [refresh]);

  const active = useMemo(() => activeCredentialMetadata(items), [items]);
  return { items, active, loading, error, setError, refresh };
}
