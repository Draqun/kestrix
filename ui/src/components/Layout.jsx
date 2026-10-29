import { useState, useEffect } from 'react';
import { useNavigate, useLocation } from 'react-router-dom';
import { useColorMode } from '../context/ColorModeContext';
import { useAuth } from '../context/AuthContext';
import useKeyboardNav from '../hooks/useKeyboardNav';
import {
  AppBar, Toolbar, Typography, Drawer, List, ListItem, ListItemIcon,
  ListItemText, ListItemButton, Box, IconButton, Chip, Tooltip, useMediaQuery,
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
  DarkMode as DarkModeIcon,
  LightMode as LightModeIcon,
  Logout as LogoutIcon,
} from '@mui/icons-material';
import Brand from './Brand';
import { getVersion } from '../api/client';

const DRAWER_WIDTH = 228;
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
  const navigate = useNavigate();
  const location = useLocation();
  const [version, setVersion] = useState(null);
  const { mode, toggleColorMode } = useColorMode();
  const { name, role, logout } = useAuth();
  const theme = useTheme();
  const isMobile = useMediaQuery(theme.breakpoints.down('sm'));
  const isTablet = useMediaQuery(theme.breakpoints.between('sm', 'md'));
  const isDesktop = useMediaQuery(theme.breakpoints.up('md'));
  const [drawerOpen, setDrawerOpen] = useState(isDesktop);

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
    <List sx={{ px: 1.5, pt: 3 }}>
      {NAV_ITEMS.map((item) => (
        <Tooltip key={item.path} title={`${item.label} (${item.shortcut})`} placement="right">
          <ListItem
            disablePadding
            aria-current={isActive(location, item.path) ? 'page' : undefined}
          >
            <ListItemButton
              onClick={() => { navigate(item.path); if (isTablet) setDrawerOpen(false); }}
              sx={{
                borderRadius: 1, mb: 0.5, minHeight: 44, px: 1.5,
                bgcolor: isActive(location, item.path) ? 'action.selected' : 'transparent',
                '& .MuiListItemText-primary': { fontSize: 14, fontWeight: isActive(location, item.path) ? 600 : 400 },
              }}
            >
            <ListItemIcon sx={{ minWidth: isTablet && !drawerOpen ? 0 : 32, justifyContent: 'center', '& svg': { fontSize: 19 } }}>
              {item.icon}
            </ListItemIcon>
            {(!isTablet || drawerOpen) && <ListItemText primary={item.label} />}
            </ListItemButton>
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
        bgcolor: 'background.default', color: 'text.primary', boxShadow: 'none',
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
          <Box sx={{ flexGrow: 1 }}><Brand /></Box>
          <Typography variant="body2" color="text.secondary" sx={{ display: { xs: 'none', md: 'block' }, mr: 3 }}>
            CI/CD control plane
          </Typography>
          <IconButton
            color="inherit"
            aria-label={`Switch to ${mode === 'dark' ? 'light' : 'dark'} mode`}
            onClick={toggleColorMode}
            sx={{ mr: 1 }}
          >
            {mode === 'dark' ? <LightModeIcon /> : <DarkModeIcon />}
          </IconButton>
          {version && <Chip
            label={`v${version}`}
            size="small"
            color="secondary"
            variant="outlined"
            sx={{ mr: 1, display: { xs: 'none', sm: 'flex' } }}
          />}
          {name && (
            <Tooltip title={`Signed in as ${name} (${role})`}>
              <Chip label={name} size="small" variant="outlined" color="inherit" sx={{ mr: 1, maxWidth: { xs: 84, sm: 160 } }} />
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
              bgcolor: 'background.default', border: 0,
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
          minWidth: 0,
          p: { xs: 2, sm: 3 },
          pb: isMobile ? 10 : 3,
          mt: { xs: '56px', sm: '64px' },
          mr: { xs: 0, sm: 1 }, mb: { xs: 0, sm: 1 },
          bgcolor: 'background.paper',
          border: { xs: 0, sm: `1px solid ${theme.palette.divider}` },
          borderRadius: { xs: 0, sm: 2 },
        }}
      >
        {children}
      </Box>

      {/* Mobile Bottom Navigation */}
      {isMobile && (
        <Paper
          sx={{ borderTop: '1px solid', borderColor: 'divider', position: 'fixed', bottom: 0, left: 0, right: 0, zIndex: theme.zIndex.appBar }}
          elevation={0}
        >
          <BottomNavigation
            value={NAV_ITEMS.findIndex(item => isActive(location, item.path))}
            onChange={(_, index) => navigate(NAV_ITEMS[index].path)}
            showLabels
            sx={{ px: 2, height: 64 }}
          >
            {NAV_ITEMS.map((item) => (
              <BottomNavigationAction
                key={item.path}
                label={item.mobileLabel || item.label}
                icon={item.icon}
                sx={{ minWidth: 0, px: 0.5, '& .MuiBottomNavigationAction-label': { fontSize: 11, '&.Mui-selected': { fontSize: 11 } } }}
              />
            ))}
          </BottomNavigation>
        </Paper>
      )}
    </Box>
  );
}
