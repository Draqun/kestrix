import React from 'react';
import {
  Box,
  Button,
  FormControl,
  InputLabel,
  MenuItem,
  Select,
  Stack,
  Typography,
} from '@mui/material';
import { ChevronLeft, ChevronRight } from '@mui/icons-material';

/**
 * Pagination controls for list views.
 *
 * Props:
 * - pagination: { total, limit, offset, next?, prev? }
 * - onPageChange: (newOffset: number) => void
 * - onLimitChange?: (newLimit: number) => void
 */
export default function Pagination({ pagination, onPageChange, onLimitChange }) {
  if (!pagination) return null;

  const { total, limit, offset, next, prev } = pagination;
  const currentPage = Math.floor(offset / limit) + 1;
  const totalPages = Math.ceil(total / limit) || 1;

  const clampOffset = (value) => {
    if (value < 0) return 0;
    if (value > total) return total;
    return value;
  };

  const parseOffsetFromUrl = (urlString) => {
    if (!urlString) return null;
    try {
      const url = new URL(urlString, window.location.origin);
      return Number(url.searchParams.get('offset'));
    } catch {
      return null;
    }
  };

  const handlePrev = () => {
    const urlOffset = parseOffsetFromUrl(prev);
    const newOffset = urlOffset !== null ? urlOffset : offset - limit;
    onPageChange(clampOffset(newOffset));
  };

  const handleNext = () => {
    const urlOffset = parseOffsetFromUrl(next);
    const newOffset = urlOffset !== null ? urlOffset : offset + limit;
    onPageChange(clampOffset(newOffset));
  };

  const handleLimitChange = (event) => {
    if (onLimitChange) {
      onLimitChange(Number(event.target.value));
    }
  };

  return (
    <Box sx={{ display: 'flex', justifyContent: 'center', mt: 3, mb: 2 }}>
      <Stack
        direction="row"
        spacing={2}
        alignItems="center"
        sx={{
          px: 2,
          py: 1,
          borderRadius: 1,
          border: (theme) => `1px solid ${theme.palette.divider}`,
          bgcolor: (theme) => theme.palette.background.paper,
        }}
      >
        <Button
          variant="outlined"
          size="small"
          startIcon={<ChevronLeft />}
          onClick={handlePrev}
          disabled={offset <= 0}
        >
          Previous
        </Button>

        <Typography
          variant="body2"
          sx={{ minWidth: 110, textAlign: 'center', fontWeight: 600 }}
        >
          Page {currentPage} of {totalPages}
        </Typography>

        <Button
          variant="outlined"
          size="small"
          endIcon={<ChevronRight />}
          onClick={handleNext}
          disabled={offset + limit >= total}
        >
          Next
        </Button>

        {onLimitChange && (
          <FormControl size="small" sx={{ minWidth: 100 }}>
            <InputLabel id="per-page-label">Per page</InputLabel>
            <Select
              labelId="per-page-label"
              label="Per page"
              value={limit}
              onChange={handleLimitChange}
            >
              <MenuItem value={10}>10</MenuItem>
              <MenuItem value={25}>25</MenuItem>
              <MenuItem value={50}>50</MenuItem>
              <MenuItem value={100}>100</MenuItem>
            </Select>
          </FormControl>
        )}
      </Stack>
    </Box>
  );
}
