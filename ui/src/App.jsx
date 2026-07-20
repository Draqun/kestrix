import { BrowserRouter, Routes, Route } from 'react-router-dom';
import { ThemeProvider } from '@mui/material/styles';
import theme from './theme/theme';
import Layout from './components/Layout';
import Dashboard from './pages/Dashboard';
import PipelineEditor from './pages/PipelineEditor';
import Projects from './pages/Projects';
import WebhookLogs from './pages/WebhookLogs';

export default function App() {
  return (
    <ThemeProvider theme={theme}>
      <BrowserRouter>
        <Layout>
          <Routes>
            <Route path="/" element={<Dashboard />} />
            <Route path="/pipeline" element={<PipelineEditor />} />
            <Route path="/projects" element={<Projects />} />
            <Route path="/webhooks" element={<WebhookLogs />} />
          </Routes>
        </Layout>
      </BrowserRouter>
    </ThemeProvider>
  );
}
