import { BrowserRouter, Routes, Route } from 'react-router-dom';
import { ThemeProvider } from '@mui/material/styles';
import { useState } from 'react';
import theme from './theme/theme';
import Layout from './components/Layout';
import Dashboard from './pages/Dashboard';
import PipelineEditor from './pages/PipelineEditor';
import Projects from './pages/Projects';
import WebhookLogs from './pages/WebhookLogs';

export default function App() {
  const [activeProject, setActiveProject] = useState(null);

  const openPipeline = (name) => {
    setActiveProject(name);
    window.history.pushState({}, '', '/pipeline');
  };

  return (
    <ThemeProvider theme={theme}>
      <BrowserRouter>
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
      </BrowserRouter>
    </ThemeProvider>
  );
}
