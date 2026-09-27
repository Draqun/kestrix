import { useMemo } from 'react';
import { listCredentials } from '../api/client.js';
import { activeCredentialMetadata } from '../lib/projectForm.js';
import useListResource from './useListResource.js';

export default function useCredentials() {
  const list = useListResource(listCredentials);
  const active = useMemo(() => activeCredentialMetadata(list.items), [list.items]);
  return { ...list, active };
}
