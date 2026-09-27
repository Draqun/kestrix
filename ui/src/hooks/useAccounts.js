import { listAccounts } from '../api/client.js';
import useListResource from './useListResource.js';

export default function useAccounts(enabled = true) {
  return useListResource(listAccounts, enabled);
}
