import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { AuthProvider } from './context/AuthContext';
import { ProtectedRoute } from './components/ProtectedRoute';
import { LoginPage } from './pages/LoginPage';
import { DashboardPage } from './pages/DashboardPage';
import { ClustersPage } from './pages/ClustersPage';
import { ClusterDetailPage } from './pages/ClusterDetailPage';
import { ProvisionClusterPage } from './pages/ProvisionClusterPage';
import { NetworksPage } from './pages/NetworksPage';
import { ProxmoxPage } from './pages/ProxmoxPage';
import { UsersPage } from './pages/UsersPage';
import { TopologyPage } from './pages/TopologyPage';
import { SSHKeysPage } from './pages/SSHKeysPage';
import { SSLCertificatesPage } from './pages/SSLCertificatesPage';
import { ClusterSecretsPage } from './pages/ClusterSecretsPage';
import { ClusterConfigMapsPage } from './pages/ClusterConfigMapsPage';
import { ClusterManifestsPage } from './pages/ClusterManifestsPage';
import { DatabaseClustersPage } from './pages/DatabaseClustersPage';
import { CloudflareTunnelsPage } from './pages/CloudflareTunnelsPage';
import { CommandCenterPage } from './pages/CommandCenterPage';
import { DocumentsPage } from './pages/DocumentsPage';

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: 1,
      staleTime: 30_000,
    },
  },
});

export function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <AuthProvider>
        <BrowserRouter>
          <Routes>
            <Route path="/login" element={<LoginPage />} />
            <Route path="/" element={<Navigate to="/dashboard" replace />} />
            <Route
              path="/dashboard"
              element={
                <ProtectedRoute>
                  <DashboardPage />
                </ProtectedRoute>
              }
            />
            <Route
              path="/clusters"
              element={
                <ProtectedRoute>
                  <ClustersPage />
                </ProtectedRoute>
              }
            />
            <Route
              path="/clusters/new"
              element={
                <ProtectedRoute requiredRoles={['super_user', 'super_admin']}>
                  <ProvisionClusterPage />
                </ProtectedRoute>
              }
            />
            <Route
              path="/clusters/:id"
              element={
                <ProtectedRoute>
                  <ClusterDetailPage />
                </ProtectedRoute>
              }
            />
            <Route
              path="/database-clusters"
              element={
                <ProtectedRoute requiredRoles={['super_user', 'super_admin']}>
                  <DatabaseClustersPage />
                </ProtectedRoute>
              }
            />
            <Route
              path="/cloudflare-tunnels"
              element={
                <ProtectedRoute requiredRoles={['super_user', 'super_admin']}>
                  <CloudflareTunnelsPage />
                </ProtectedRoute>
              }
            />
            <Route
              path="/commands"
              element={
                <ProtectedRoute requiredRoles={['super_user', 'super_admin']}>
                  <CommandCenterPage />
                </ProtectedRoute>
              }
            />
            <Route
              path="/networks"
              element={
                <ProtectedRoute>
                  <NetworksPage />
                </ProtectedRoute>
              }
            />
            <Route
              path="/proxmox"
              element={
                <ProtectedRoute>
                  <ProxmoxPage />
                </ProtectedRoute>
              }
            />
            <Route
              path="/users"
              element={
                <ProtectedRoute requiredRoles={['super_user', 'super_admin']}>
                  <UsersPage />
                </ProtectedRoute>
              }
            />
            <Route
              path="/ssh-keys"
              element={
                <ProtectedRoute requiredRoles={['super_user', 'super_admin']}>
                  <SSHKeysPage />
                </ProtectedRoute>
              }
            />
            <Route
              path="/ssl-certificates"
              element={
                <ProtectedRoute requiredRoles={['super_user', 'super_admin']}>
                  <SSLCertificatesPage />
                </ProtectedRoute>
              }
            />
            <Route
              path="/secrets"
              element={
                <ProtectedRoute requiredRoles={['super_user', 'super_admin']}>
                  <ClusterSecretsPage />
                </ProtectedRoute>
              }
            />
            <Route
              path="/configmaps"
              element={
                <ProtectedRoute requiredRoles={['super_user', 'super_admin']}>
                  <ClusterConfigMapsPage />
                </ProtectedRoute>
              }
            />
            <Route
              path="/manifests"
              element={
                <ProtectedRoute requiredRoles={['super_user', 'super_admin']}>
                  <ClusterManifestsPage />
                </ProtectedRoute>
              }
            />
            <Route
              path="/documents"
              element={
                <ProtectedRoute requiredRoles={['super_user', 'super_admin']}>
                  <DocumentsPage />
                </ProtectedRoute>
              }
            />
            <Route
              path="/documents/quick-commands"
              element={
                <ProtectedRoute requiredRoles={['super_user', 'super_admin']}>
                  <DocumentsPage category="quick-commands" />
                </ProtectedRoute>
              }
            />
            <Route
              path="/documents/database-cluster"
              element={
                <ProtectedRoute requiredRoles={['super_user', 'super_admin']}>
                  <DocumentsPage category="database-cluster" />
                </ProtectedRoute>
              }
            />
            <Route
              path="/documents/kubernetes-cluster"
              element={
                <ProtectedRoute requiredRoles={['super_user', 'super_admin']}>
                  <DocumentsPage category="kubernetes-cluster" />
                </ProtectedRoute>
              }
            />
            <Route
              path="/topology"
              element={
                <ProtectedRoute>
                  <TopologyPage />
                </ProtectedRoute>
              }
            />
            <Route path="*" element={<Navigate to="/dashboard" replace />} />
          </Routes>
        </BrowserRouter>
      </AuthProvider>
    </QueryClientProvider>
  );
}
