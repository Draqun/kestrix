export function clampOffset(value, total) {
  if (value < 0) return 0;
  if (value > total) return total;
  return value;
}

export function parseOffsetFromUrl(urlString) {
  if (!urlString) return null;
  try {
    const url = new URL(urlString, typeof window !== 'undefined' ? window.location.origin : 'http://localhost');
    const val = url.searchParams.get('offset');
    if (val === null) return null;
    return Number(val);
  } catch {
    return null;
  }
}

export function getPaginationInfo(pagination) {
  const { total, limit, offset } = pagination;
  const currentPage = Math.floor(offset / limit) + 1;
  const totalPages = Math.ceil(total / limit) || 1;
  return { currentPage, totalPages };
}