import { BrowserRouter, Routes, Route, useNavigate, useParams } from 'react-router-dom';
import { ThemeProvider } from '@mui/material/styles';
import theme from './theme/theme';
import Layout from './components/Layout';
import Dashboard from './pages/Dashboard';
import Pipelines from './pages/Pipelines';
import Projects from './pages/Projects';
import WebhookLogs from './pages/WebhookLogs';

function ProjectsRoutes() {
  const { name } = useParams();
  const navigate = useNavigate();
  return <Projects selectedName={name} onSelect={(n) => n ? navigate(`/projects/${encodeURIComponent(n)}`) : navigate('/projects')} />;
}

function AppRoutes() {
  const navigate = useNavigate();

  const openPipeline = (name) => {
    navigate(`/pipelines?project=${encodeURIComponent(name)}`);
  };

  return (
    <Layout>
      <Routes>
        <Route path="/" element={<Dashboard />} />
        <Route path="/pipelines" element={<Pipelines />} />
        <Route path="/projects" element={<Projects selectedName={null} onSelect={(n) => n ? navigate(`/projects/${encodeURIComponent(n)}`) : navigate('/projects')} />} />
        <Route path="/projects/:name" element={<ProjectsRoutes />} />
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
