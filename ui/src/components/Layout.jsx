import { useState, useEffect } from 'react';
import { useNavigate, useLocation } from 'react-router-dom';
import { useColorMode } from '../context/ColorModeContext';
import { useAuth } from '../context/AuthContext';
import useKeyboardNav from '../hooks/useKeyboardNav';
import {
  AppBar, Toolbar, Typography, Drawer, List, ListItem, ListItemIcon,
  ListItemText, Box, IconButton, Chip, Tooltip, useMediaQuery,
  BottomNavigation, BottomNavigationAction, Paper,
} from '@mui/material';
import { useTheme } from '@mui/material/styles';
import {
  Dashboard as DashboardIcon,
  AccountTree as DagIcon,
  Folder as ProjectsIcon,
  Webhook as WebhookIcon,
  Settings as SettingsIcon,
  Menu as MenuIcon,
  MusicNote as MusicNoteIcon,
  DarkMode as DarkModeIcon,
  LightMode as LightModeIcon,
  Logout as LogoutIcon,
} from '@mui/icons-material';
import { getVersion } from '../api/client';

const DRAWER_WIDTH = 260;
const MINI_DRAWER_WIDTH = 72;

const NAV_ITEMS = [
  { path: '/', label: 'Dashboard', icon: <DashboardIcon />, shortcut: 'g+d' },
  { path: '/projects', label: 'Projects', icon: <ProjectsIcon />, shortcut: 'g+r' },
  { path: '/pipelines', label: 'Pipelines', icon: <DagIcon />, shortcut: 'g+p' },
  { path: '/webhooks', label: 'Webhook Logs', mobileLabel: 'Webhooks', icon: <WebhookIcon />, shortcut: 'g+w' },
  { path: '/settings', label: 'Settings', icon: <SettingsIcon />, shortcut: 'g+s' },
];

function isActive(location, path) {
  if (path === '/') return location.pathname === '/';
  return location.pathname.startsWith(path);
}

export default function Layout({ children }) {
  const [drawerOpen, setDrawerOpen] = useState(true);
  const navigate = useNavigate();
  const location = useLocation();
  const [version, setVersion] = useState(null);
  const { mode, toggleColorMode } = useColorMode();
  const { name, role, logout } = useAuth();
  const theme = useTheme();
  const isMobile = useMediaQuery(theme.breakpoints.down('sm'));
  const isTablet = useMediaQuery(theme.breakpoints.between('sm', 'md'));
  const isDesktop = useMediaQuery(theme.breakpoints.up('md'));

  useEffect(() => {
    getVersion().then(v => {
      if (v && v.version) setVersion(v.version);
    }).catch(() => {});
  }, []);

  // Desktop: persistent/mini drawer
  // Tablet: mini-drawer with hover expansion
  // Mobile: bottom navigation
  useKeyboardNav(navigate);

  const navList = (
    <List>
      {NAV_ITEMS.map((item) => (
        <Tooltip key={item.path} title={`${item.label} (${item.shortcut})`} placement="right">
          <ListItem
            onClick={() => navigate(item.path)}
            aria-current={isActive(location, item.path) ? 'page' : undefined}
            sx={{
              cursor: 'pointer',
              backgroundColor: isActive(location, item.path)
                ? 'action.selected' : 'transparent',
              '&:hover': { backgroundColor: 'action.hover' },
              justifyContent: (isTablet && !drawerOpen) ? 'center' : undefined,
              px: (isTablet && !drawerOpen) ? 0 : 2,
            }}
          >
            <ListItemIcon sx={{ minWidth: isTablet && !drawerOpen ? 0 : 56, justifyContent: 'center' }}>
              {item.icon}
            </ListItemIcon>
            {(!isTablet || drawerOpen) && <ListItemText primary={item.label} />}
          </ListItem>
        </Tooltip>
      ))}
    </List>
  );

  return (
    <Box sx={{ display: 'flex', minHeight: '100vh' }}>
      {/* App Bar */}
      <AppBar position="fixed" sx={{
        zIndex: (theme) => theme.zIndex.drawer + 1,
        background: (theme) => theme.palette.mode === 'dark'
          ? 'linear-gradient(135deg, #1e1e2e 0%, #2a2a3e 100%)'
          : undefined,
      }}>
        <Toolbar>
          {!isMobile && (
            <IconButton
              color="inherit"
              edge="start"
              aria-label="Toggle navigation drawer"
              onClick={() => setDrawerOpen(!drawerOpen)}
              sx={{ mr: 2 }}
            >
              <MenuIcon />
            </IconButton>
          )}
          <MusicNoteIcon sx={{ mr: 1 }} />
          <Typography variant="h6" noWrap sx={{ flexGrow: 1 }}>
            Kestrix
          </Typography>
          <IconButton
            color="inherit"
            aria-label={`Switch to ${mode === 'dark' ? 'light' : 'dark'} mode`}
            onClick={toggleColorMode}
            sx={{ mr: 1 }}
          >
            {mode === 'dark' ? <LightModeIcon /> : <DarkModeIcon />}
          </IconButton>
          <Chip
            label={version ? `v${version}` : ''}
            size="small"
            color="secondary"
            variant="outlined"
            sx={{ mr: 1 }}
          />
          {name && (
            <Tooltip title={`Signed in as ${name} (${role})`}>
              <Chip label={name} size="small" variant="outlined" color="inherit" sx={{ mr: 1 }} />
            </Tooltip>
          )}
          <Tooltip title="Sign out">
            <IconButton color="inherit" aria-label="Sign out" onClick={logout}>
              <LogoutIcon />
            </IconButton>
          </Tooltip>
        </Toolbar>
      </AppBar>

      {/* Desktop / Tablet Sidebar */}
      {!isMobile && (
        <Drawer
          variant={isDesktop ? "persistent" : "temporary"}
          anchor="left"
          open={drawerOpen}
          onClose={() => setDrawerOpen(false)}
          sx={{
            width: isDesktop ? (drawerOpen ? DRAWER_WIDTH : 0) : (drawerOpen ? DRAWER_WIDTH : 0),
            flexShrink: 0,
            '& .MuiDrawer-paper': {
              width: isTablet && !drawerOpen ? MINI_DRAWER_WIDTH : DRAWER_WIDTH,
              boxSizing: 'border-box',
              overflowX: 'hidden',
              transition: theme.transitions.create('width', {
                easing: theme.transitions.easing.sharp,
                duration: theme.transitions.duration.enteringScreen,
              }),
            },
          }}
        >
          <Toolbar />
          <Box sx={{ overflow: 'auto' }}>
            {isTablet && !drawerOpen ? (
              <Tooltip title={NAV_ITEMS.map(i => i.label).join(' · ')} placement="right">
                <Box>{navList}</Box>
              </Tooltip>
            ) : navList}
          </Box>
        </Drawer>
      )}

      {/* Main content */}
      <Box
        component="main"
        sx={{
          flexGrow: 1,
          p: 3,
          pb: isMobile ? 10 : 3,
          width: '100%',
          overflow: 'auto',
        }}
      >
        <Toolbar />
        {children}
      </Box>

      {/* Mobile Bottom Navigation */}
      {isMobile && (
        <Paper
          sx={{ position: 'fixed', bottom: 0, left: 0, right: 0, zIndex: theme.zIndex.appBar }}
          elevation={3}
        >
          <BottomNavigation
            value={NAV_ITEMS.findIndex(item => isActive(location, item.path))}
            onChange={(_, index) => navigate(NAV_ITEMS[index].path)}
            showLabels
          >
            {NAV_ITEMS.map((item) => (
              <BottomNavigationAction
                key={item.path}
                label={item.mobileLabel || item.label}
                icon={item.icon}
                sx={{ minWidth: 0, px: 0.5 }}
              />
            ))}
          </BottomNavigation>
        </Paper>
      )}
    </Box>
  );
}
