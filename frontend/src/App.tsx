import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { useAuth } from './hooks/useAuth';
import Layout from './components/Layout';
import LoginPage from './pages/LoginPage';
import DashboardPage from './pages/DashboardPage';
import ServersPage from './pages/ServersPage';
import ServerDetailPage from './pages/ServerDetailPage';
import JobsPage from './pages/JobsPage';
import SnapshotsPage from './pages/SnapshotsPage';
import RecoveryPage from './pages/RecoveryPage';
import AssistantPage from './pages/AssistantPage';

const queryClient = new QueryClient();

function PlaceholderPage({ title }: { title: string }) {
  return (
    <div className="p-6">
      <h1 className="text-2xl font-bold">{title}</h1>
      <p className="text-gray-500 mt-2">This page is under construction.</p>
    </div>
  );
}

function AppRoutes() {
  const { user, isAuthenticated, logout } = useAuth();

  return (
    <Routes>
      <Route
        path="/login"
        element={isAuthenticated ? <Navigate to="/" replace /> : <LoginPage />}
      />

      {isAuthenticated && user ? (
        <Route element={<Layout user={user} onLogout={logout} />}>
          <Route path="/" element={<DashboardPage />} />
          <Route path="/servers" element={<ServersPage />} />
          <Route path="/servers/:id" element={<ServerDetailPage />} />
          <Route path="/jobs" element={<JobsPage />} />
          <Route path="/snapshots" element={<SnapshotsPage />} />
          <Route path="/recovery" element={<RecoveryPage />} />
          <Route path="/docs" element={<PlaceholderPage title="Docs" />} />
          <Route path="/assistant" element={<AssistantPage />} />
          <Route path="/settings" element={<PlaceholderPage title="Settings" />} />
          <Route path="/audit" element={<PlaceholderPage title="Audit Log" />} />
          <Route path="*" element={<Navigate to="/" replace />} />
        </Route>
      ) : (
        <Route path="*" element={<Navigate to="/login" replace />} />
      )}
    </Routes>
  );
}

export default function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <AppRoutes />
      </BrowserRouter>
    </QueryClientProvider>
  );
}
