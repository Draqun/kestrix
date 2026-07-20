import { useState } from 'react';
import { useNavigate, useLocation } from 'react-router-dom';
import {
  AppBar, Toolbar, Typography, Drawer, List, ListItem, ListItemIcon,
  ListItemText, Box, Divider, IconButton, Chip, CssBaseline
} from '@mui/material';
import {
  Dashboard as DashboardIcon,
  AccountTree as DagIcon,
  Folder as ProjectsIcon,
  Webhook as WebhookIcon,
  Settings as SettingsIcon,
  Menu as MenuIcon,
  Science as ScienceIcon,
} from '@mui/icons-material';

const DRAWER_WIDTH = 260;

const NAV_ITEMS = [
  { path: '/', label: 'Dashboard', icon: <DashboardIcon /> },
  { path: '/projects', label: 'Projects', icon: <ProjectsIcon /> },
  { path: '/pipelines', label: 'Pipelines', icon: <DagIcon /> },
  { path: '/webhooks', label: 'Webhook Logs', icon: <WebhookIcon /> },
];

export default function Layout({ children }) {
  const [drawerOpen, setDrawerOpen] = useState(true);
  const navigate = useNavigate();
  const location = useLocation();

  return (
    <Box sx={{ display: 'flex' }}>
      <CssBaseline />

      {/* App Bar */}
      <AppBar position="fixed" sx={{ zIndex: (theme) => theme.zIndex.drawer + 1 }}>
        <Toolbar>
          <IconButton
            color="inherit"
            edge="start"
            onClick={() => setDrawerOpen(!drawerOpen)}
            sx={{ mr: 2 }}
          >
            <MenuIcon />
          </IconButton>
          <ScienceIcon sx={{ mr: 1 }} />
          <Typography variant="h6" noWrap sx={{ flexGrow: 1 }}>
            MetaCI
          </Typography>
          <Chip
            label="PoC v0.2"
            size="small"
            color="secondary"
            variant="outlined"
          />
        </Toolbar>
      </AppBar>

      {/* Sidebar */}
      <Drawer
        variant="persistent"
        anchor="left"
        open={drawerOpen}
        sx={{
          width: drawerOpen ? DRAWER_WIDTH : 0,
          flexShrink: 0,
          '& .MuiDrawer-paper': {
            width: DRAWER_WIDTH,
            boxSizing: 'border-box',
          },
        }}
      >
        <Toolbar />
        <Box sx={{ overflow: 'auto' }}>
          <List>
            {NAV_ITEMS.map((item) => (
              <ListItem
                key={item.path}
                onClick={() => navigate(item.path)}
                sx={{
                  cursor: 'pointer',
                  backgroundColor: location.pathname === item.path || (item.path !== '/' && location.pathname.startsWith(item.path))
                    ? 'action.selected' : 'transparent',
                  '&:hover': { backgroundColor: 'action.hover' },
                }}
              >
                <ListItemIcon>{item.icon}</ListItemIcon>
                <ListItemText primary={item.label} />
              </ListItem>
            ))}
          </List>
          <Divider />
          <List>
            <ListItem disabled>
              <ListItemIcon><SettingsIcon /></ListItemIcon>
              <ListItemText primary="Settings (coming soon)" />
            </ListItem>
          </List>
        </Box>
      </Drawer>

      {/* Main content */}
      <Box
        component="main"
        sx={{
          flexGrow: 1,
          p: 3,
        }}
      >
        <Toolbar />
        {children}
      </Box>
    </Box>
  );
}
