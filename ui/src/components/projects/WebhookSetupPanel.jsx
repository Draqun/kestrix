import { Box, Card, CardContent, Typography, Alert } from '@mui/material';
import CopyButton from '../CopyButton.jsx';

export default function WebhookSetupPanel({ hasProjectSecret }) {
  // The server has no configured public URL, so the browser's origin is the
  // best available guess at what the Git host has to reach (design D3).
  const url = `${window.location.origin}/webhook`;

  return (
    <Card sx={{ mt: 2 }}>
      <CardContent>
        <Typography variant="h6" sx={{ mb: 1 }}>Webhook setup</Typography>
        <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
          Point your Git host at this URL to start pipelines on push. The address is the one your
          browser is using; your Git host must be able to reach that same host.
        </Typography>

        <Box sx={{ position: 'relative', p: 1.5, pr: 5, mb: 2, border: 1, borderColor: 'divider', borderRadius: 1, bgcolor: 'action.hover' }}>
          <Typography variant="body2" component="code" data-testid="webhook-delivery-url" sx={{ wordBreak: 'break-all' }}>
            {url}
          </Typography>
          <CopyButton text={url} />
        </Box>

        <Typography variant="subtitle2" color="text.secondary" gutterBottom>In GitLab (token mode)</Typography>
        <Box component="ol" sx={{ pl: 2.5, m: 0, mb: 2 }}>
          <Typography component="li" variant="body2">Open <strong>Settings → Webhooks</strong> on the repository.</Typography>
          <Typography component="li" variant="body2">Paste the URL above into <strong>URL</strong>.</Typography>
          <Typography component="li" variant="body2">Put this project's webhook secret into <strong>Secret token</strong>.</Typography>
          <Typography component="li" variant="body2">Tick <strong>Push events</strong>, then add the webhook.</Typography>
        </Box>

        <Typography variant="body2" color="text.secondary">
          By default this server verifies the <code>X-Gitlab-Token</code> header (token mode) - the
          steps above are what GitLab&apos;s own webhook UI sends. If your operator switched this
          server to HMAC mode, GitLab cannot be used as-is: GitLab does not send an HMAC signature.
          HMAC mode verifies <code>X-Hub-Signature-256</code> instead, so it needs a Git host that
          signs deliveries that way using this project&apos;s webhook secret as the signing key.
          The server&apos;s configured method decides which one is verified for GitLab.
        </Typography>

        <Typography variant="subtitle2" color="text.secondary" gutterBottom sx={{ mt: 2 }}>In GitHub</Typography>
        <Box component="ol" sx={{ pl: 2.5, m: 0, mb: 2 }}>
          <Typography component="li" variant="body2">Open <strong>Settings → Webhooks → Add webhook</strong> on the repository.</Typography>
          <Typography component="li" variant="body2">Paste the URL above into <strong>Payload URL</strong>.</Typography>
          <Typography component="li" variant="body2">Set <strong>Content type</strong> to <code>application/json</code>.</Typography>
          <Typography component="li" variant="body2">Put this project's webhook secret into <strong>Secret</strong>.</Typography>
          <Typography component="li" variant="body2">Select <strong>Just the push event</strong>, then add the webhook.</Typography>
        </Box>

        <Typography variant="body2" color="text.secondary">
          GitHub deliveries are always verified by <code>X-Hub-Signature-256</code> using HMAC-SHA256,
          regardless of the server&apos;s configured method.
        </Typography>

        {!hasProjectSecret && (
          <Alert severity="warning" sx={{ mt: 2 }}>
            This project has no webhook secret, so deliveries are verified against the instance-wide
            secret. Set one in <strong>Edit project</strong> to scope verification to this project.
          </Alert>
        )}
      </CardContent>
    </Card>
  );
}
