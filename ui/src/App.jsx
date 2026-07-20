import { BrowserRouter, Routes, Route, useNavigate } from 'react-router-dom';
import { ThemeProvider } from '@mui/material/styles';
import { useState } from 'react';
import theme from './theme/theme';
import Layout from './components/Layout';
import Dashboard from './pages/Dashboard';
import PipelineEditor from './pages/PipelineEditor';
import Projects from './pages/Projects';
import WebhookLogs from './pages/WebhookLogs';

function AppRoutes() {
  const [activeProject, setActiveProject] = useState(null);
  const navigate = useNavigate();

  const openPipeline = (name) => {
    setActiveProject(name);
    navigate('/pipeline');
  };

  return (
    <Layout>
      <Routes>
        <Route path="/" element={<Dashboard />} />
        <Route
          path="/pipeline"
          element={<PipelineEditor projectName={activeProject} />}
        />
        <Route
          path="/projects"
          element={<Projects onOpenPipeline={openPipeline} />}
        />
        <Route path="/webhooks" element={<WebhookLogs />} />
      </Routes>
    </Layout>
  );
}

export default function App() {
  return (
    <ThemeProvider theme={theme}>
      <BrowserRouter>
        <AppRoutes />
      </BrowserRouter>
    </ThemeProvider>
  );
}
