import { listRunners } from '../api/client.js';
import useListResource from './useListResource.js';

export default function useRunners(enabled = true) {
  return useListResource(listRunners, enabled);
}
