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
import { clampOffset, parseOffsetFromUrl, getPaginationInfo } from '../lib/paginationUtils.js';

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
  const { currentPage, totalPages } = getPaginationInfo(pagination);

  const handlePrev = () => {
    const urlOffset = parseOffsetFromUrl(prev);
    const newOffset = urlOffset !== null ? urlOffset : offset - limit;
    onPageChange(clampOffset(newOffset, total));
  };

  const handleNext = () => {
    const urlOffset = parseOffsetFromUrl(next);
    const newOffset = urlOffset !== null ? urlOffset : offset + limit;
    onPageChange(clampOffset(newOffset, total));
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

        <Box
          sx={{
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            minWidth: 110,
          }}
        >
          <Typography variant="body2" sx={{ fontWeight: 600 }}>
            Page {currentPage} of {totalPages}
          </Typography>
        </Box>

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
