import React from 'react';
import { Button, Box, Typography } from '@mui/material';

/**
 * Simple pagination component.
 * Props:
 * - pagination: { total, limit, offset, next, prev }
 * - onPageChange: (newOffset) => void
 */
export default function Pagination({ pagination, onPageChange }) {
  if (!pagination) return null;
  const { total, limit, offset, next, prev } = pagination;
  const currentPage = Math.floor(offset / limit) + 1;
  const totalPages = Math.ceil(total / limit) || 1;

  const handlePrev = () => {
    if (prev) {
      const url = new URL(prev, window.location.origin);
      const newOffset = Number(url.searchParams.get('offset')) || 0;
      onPageChange(newOffset);
    }
  };

  const handleNext = () => {
    if (next) {
      const url = new URL(next, window.location.origin);
      const newOffset = Number(url.searchParams.get('offset')) || 0;
      onPageChange(newOffset);
    }
  };

  return (
    <Box display="flex" alignItems="center" justifyContent="center" mt={2} gap={2}>
      <Button variant="outlined" size="small" onClick={handlePrev} disabled={!prev}>
        Prev
      </Button>
      <Typography variant="body2">
        Page {currentPage} of {totalPages}
      </Typography>
      <Button variant="outlined" size="small" onClick={handleNext} disabled={!next}>
        Next
      </Button>
    </Box>
  );
}
