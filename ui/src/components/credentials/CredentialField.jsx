import InfoOutlinedIcon from '@mui/icons-material/InfoOutlined';
import { IconButton, Stack, TextField, Tooltip } from '@mui/material';

export function CredentialFieldInfo({ descriptor }) {
  return (
    <Tooltip
      arrow
      describeChild
      enterTouchDelay={0}
      title={descriptor.tooltip}
    >
      <IconButton
        aria-label={`Show guidance for ${descriptor.infoLabel.toLowerCase()}`}
        size="small"
        sx={{ flexShrink: 0, mt: 0.25 }}
      >
        <InfoOutlinedIcon fontSize="small" />
      </IconButton>
    </Tooltip>
  );
}

export default function CredentialField({ descriptor, value, onChange, children, ...props }) {
  const fieldID = `credential-${descriptor.key}`;
  return (
    <Stack direction="row" spacing={0.5} sx={{ alignItems: 'flex-start', minWidth: 0 }}>
      <TextField
        id={fieldID}
        label={descriptor.label}
        value={value}
        onChange={onChange}
        required={descriptor.required}
        helperText={descriptor.helperText}
        placeholder={descriptor.placeholder}
        type={descriptor.type}
        multiline={descriptor.multiline}
        minRows={descriptor.minRows}
        select={descriptor.select}
        size="small"
        fullWidth
        sx={{ minWidth: 0 }}
        {...props}
      >
        {children}
      </TextField>
      <CredentialFieldInfo descriptor={descriptor} />
    </Stack>
  );
}
