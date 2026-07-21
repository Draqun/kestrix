import { useState } from 'react';
import { IconButton, Tooltip } from '@mui/material';
import { ContentCopy as CopyIcon, Check as CheckIcon } from '@mui/icons-material';

export default function CopyButton({ text }) {
  const [copied, setCopied] = useState(false);

  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(text);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {}
  };

  return (
    <Tooltip title={copied ? 'Copied!' : 'Copy to clipboard'}>
      <IconButton size="small" onClick={handleCopy} sx={{ position: 'absolute', top: 4, right: 4, zIndex: 1 }}>
        {copied ? <CheckIcon fontSize="small" color="success" /> : <CopyIcon fontSize="small" />}
      </IconButton>
    </Tooltip>
  );
}
