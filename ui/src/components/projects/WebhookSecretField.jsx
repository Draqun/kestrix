import { useState } from 'react';
import { TextField, InputAdornment, IconButton } from '@mui/material';
import {
  Visibility as ShowIcon,
  VisibilityOff as HideIcon,
} from '@mui/icons-material';

// Shared by both dialogs. A stored secret is never read back from the API,
// so this field always starts empty. An empty value means different things
// per dialog: on create it means "no per-project secret, use the global
// one"; on edit it means "keep the stored secret unchanged".
export default function WebhookSecretField({ value, onChange, helperText, ...props }) {
  const [reveal, setReveal] = useState(false);
  return (
    <TextField
      label="Webhook secret (optional)"
      type={reveal ? 'text' : 'password'}
      value={value}
      onChange={(e) => onChange(e.target.value)}
      size="small"
      fullWidth
      autoComplete="new-password"
      helperText={helperText}
      slotProps={{
        input: {
          endAdornment: (
            <InputAdornment position="end">
              <IconButton
                size="small"
                aria-label={reveal ? 'Hide webhook secret' : 'Show webhook secret'}
                onClick={() => setReveal(prev => !prev)}
                edge="end"
              >
                {reveal ? <HideIcon fontSize="small" /> : <ShowIcon fontSize="small" />}
              </IconButton>
            </InputAdornment>
          ),
        },
      }}
      {...props}
    />
  );
}
