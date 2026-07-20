import { BrowserRouter, Routes, Route, useNavigate, useParams } from 'react-router-dom';
import { ThemeProvider } from '@mui/material/styles';
import theme from './theme/theme';
import Layout from './components/Layout';
import Dashboard from './pages/Dashboard';
import { RunList, NewRun, RunProject } from './pages/Pipelines';
import Projects from './pages/Projects';
import WebhookLogs from './pages/WebhookLogs';

function ProjectsRoutes() {
  const { name } = useParams();
  const navigate = useNavigate();
  return <Projects selectedName={name} onSelect={(n) => n ? navigate(`/projects/${encodeURIComponent(n)}`) : navigate('/projects')} onOpenPipeline={(n) => navigate(`/pipelines/new-run/${encodeURIComponent(n)}`)} />;
}

function NewRunRoutes() {
  const { projectName } = useParams();
  const navigate = useNavigate();
  if (projectName) {
    return <RunProject projectName={decodeURIComponent(projectName)} onBack={() => navigate('/pipelines/new-run')} />;
  }
  return <NewRun onSelect={(n) => navigate(`/pipelines/new-run/${encodeURIComponent(n)}`)} onBack={() => navigate('/pipelines')} />;
}

function AppRoutes() {
  const navigate = useNavigate();

  return (
    <Layout>
      <Routes>
        <Route path="/" element={<Dashboard />} />
        <Route path="/pipelines" element={<RunList onNewRun={() => navigate('/pipelines/new-run')} />} />
        <Route path="/pipelines/new-run" element={<NewRunRoutes />} />
        <Route path="/pipelines/new-run/:projectName" element={<NewRunRoutes />} />
        <Route path="/projects" element={<Projects selectedName={null} onSelect={(n) => n ? navigate(`/projects/${encodeURIComponent(n)}`) : navigate('/projects')} onOpenPipeline={(n) => navigate(`/pipelines/new-run/${encodeURIComponent(n)}`)} />} />
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
