import {
  Box, Button, FormControl, InputLabel, MenuItem, Select,
  Typography, Chip, Popover, IconButton, useMediaQuery,
} from '@mui/material';
import { useTheme } from '@mui/material/styles';
import { ChevronLeft, ChevronRight, Settings as SettingsIcon } from '@mui/icons-material';
import { useState } from 'react';
import { clampOffset, parseOffsetFromUrl, getPaginationInfo } from '../lib/paginationUtils.js';

export default function Pagination({ pagination, onPageChange, onLimitChange }) {
  if (!pagination) return null;

  const { total, limit, offset, next, prev } = pagination;
  const { currentPage, totalPages } = getPaginationInfo(pagination);
  const theme = useTheme();
  const isMobile = useMediaQuery(theme.breakpoints.down('sm'));

  const [anchorEl, setAnchorEl] = useState(null);

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
    setAnchorEl(null);
  };

  return (
    <Box sx={{ display: 'flex', justifyContent: 'center', mt: 3, mb: 2 }}>
      <Box
        sx={{
          display: 'flex',
          alignItems: 'center',
          gap: isMobile ? 1 : 2,
          px: 2, py: 1,
          borderRadius: 1,
          border: (theme) => `1px solid ${theme.palette.divider}`,
          bgcolor: (theme) => theme.palette.background.paper,
          flexWrap: 'wrap',
          justifyContent: 'center',
        }}
      >
        <Button
          variant="outlined"
          size="small"
          {...(isMobile ? {} : { startIcon: <ChevronLeft /> })}
          onClick={handlePrev}
          disabled={offset <= 0}
          sx={{ minWidth: isMobile ? 40 : undefined }}
        >
          {isMobile ? <ChevronLeft /> : 'Previous'}
        </Button>

        <Typography variant="body2" sx={{ fontWeight: 600, whiteSpace: 'nowrap' }}>
          {isMobile ? `${currentPage}/${totalPages}` : `Page ${currentPage} of ${totalPages}`}
        </Typography>

        <Button
          variant="outlined"
          size="small"
          {...(isMobile ? {} : { endIcon: <ChevronRight /> })}
          onClick={handleNext}
          disabled={offset + limit >= total}
          sx={{ minWidth: isMobile ? 40 : undefined }}
        >
          {isMobile ? <ChevronRight /> : 'Next'}
        </Button>

        {onLimitChange && (
          isMobile ? (
            <>
              <Chip
                label={`${limit}`}
                size="small"
                variant="outlined"
                clickable
                icon={<SettingsIcon />}
                onClick={(e) => setAnchorEl(e.currentTarget)}
              />
              <Popover
                open={!!anchorEl}
                anchorEl={anchorEl}
                onClose={() => setAnchorEl(null)}
                anchorOrigin={{ vertical: 'top', horizontal: 'center' }}
                transformOrigin={{ vertical: 'bottom', horizontal: 'center' }}
              >
                <Box sx={{ p: 1 }}>
                  {[10, 25, 50, 100].map((v) => (
                    <Chip
                      key={v}
                      label={v}
                      size="small"
                      color={v === limit ? 'primary' : 'default'}
                      variant={v === limit ? 'filled' : 'outlined'}
                      onClick={() => handleLimitChange({ target: { value: v } })}
                      sx={{ m: 0.5 }}
                    />
                  ))}
                </Box>
              </Popover>
            </>
          ) : (
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
          )
        )}
      </Box>
    </Box>
  );
}
