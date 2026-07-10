import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { AuthProvider } from './context/AuthContext';
import { ProtectedRoute } from './components/ProtectedRoute';
import { LoginPage } from './pages/LoginPage';
import { RegisterPage } from './pages/RegisterPage';
import { ResetPasswordPage } from './pages/ResetPasswordPage';
import { VerifyEmailPage } from './pages/VerifyEmailPage';
import { ProfileSettingsPage } from './pages/ProfileSettingsPage';
import { DashboardPage } from './pages/DashboardPage';
import { ClustersPage } from './pages/ClustersPage';
import { ClusterDetailPage } from './pages/ClusterDetailPage';
import { ProvisionClusterPage } from './pages/ProvisionClusterPage';
import { NetworksPage } from './pages/NetworksPage';
import { RegionsPage } from './pages/RegionsPage';
import { ProxmoxPage } from './pages/ProxmoxPage';
import { UsersPage } from './pages/UsersPage';
import { TopologyPage } from './pages/TopologyPage';
import { SSHKeysPage } from './pages/SSHKeysPage';
import { SSLCertificatesPage } from './pages/SSLCertificatesPage';
import { ClusterSecretsPage } from './pages/ClusterSecretsPage';
import { ClusterImagePullSecretsPage } from './pages/ClusterImagePullSecretsPage';
import { ClusterConfigMapsPage } from './pages/ClusterConfigMapsPage';
import { ClusterManifestsPage } from './pages/ClusterManifestsPage';
import { DatabaseClustersPage } from './pages/DatabaseClustersPage';
import { CloudflareTunnelsPage } from './pages/CloudflareTunnelsPage';
import { ProxacloudTunnelsPage } from './pages/ProxacloudTunnelsPage';
import { FirewallsPage } from './pages/FirewallsPage';
import { CommandCenterPage } from './pages/CommandCenterPage';
import { DocumentsPage } from './pages/DocumentsPage';
import { MonitoringPage } from './pages/MonitoringPage';
import { VPSPage } from './pages/VPSPage';
import { ProvisionVPSPage } from './pages/ProvisionVPSPage';
import { VPSDetailPage } from './pages/VPSDetailPage';
import { VPSConsolePage } from './pages/VPSConsolePage';
import { ContainersPage } from './pages/ContainersPage';
import { ProvisionContainerPage } from './pages/ProvisionContainerPage';
import { ContainerDetailPage } from './pages/ContainerDetailPage';
import { StoragePage } from './pages/StoragePage';
import { BackupsPage } from './pages/BackupsPage';
import { BillingPage } from './pages/BillingPage';

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
            <Route path="/register" element={<RegisterPage />} />
            <Route path="/reset-password" element={<ResetPasswordPage />} />
            <Route path="/verify-email" element={<VerifyEmailPage />} />
            <Route path="/" element={<Navigate to="/dashboard" replace />} />
            <Route
              path="/profile/settings"
              element={
                <ProtectedRoute>
                  <ProfileSettingsPage />
                </ProtectedRoute>
              }
            />
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
                <ProtectedRoute>
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
                <ProtectedRoute>
                  <DatabaseClustersPage />
                </ProtectedRoute>
              }
            />
            <Route
              path="/vps"
              element={
                <ProtectedRoute>
                  <VPSPage />
                </ProtectedRoute>
              }
            />
            <Route
              path="/vps/new"
              element={
                <ProtectedRoute>
                  <ProvisionVPSPage />
                </ProtectedRoute>
              }
            />
            <Route
              path="/vps/:id"
              element={
                <ProtectedRoute>
                  <VPSDetailPage />
                </ProtectedRoute>
              }
            />
            <Route
              path="/vps/:id/console"
              element={
                <ProtectedRoute>
                  <VPSConsolePage />
                </ProtectedRoute>
              }
            />
            <Route
              path="/containers"
              element={
                <ProtectedRoute>
                  <ContainersPage />
                </ProtectedRoute>
              }
            />
            <Route
              path="/containers/new"
              element={
                <ProtectedRoute>
                  <ProvisionContainerPage />
                </ProtectedRoute>
              }
            />
            <Route
              path="/containers/:id"
              element={
                <ProtectedRoute>
                  <ContainerDetailPage />
                </ProtectedRoute>
              }
            />
            <Route
              path="/billing"
              element={
                <ProtectedRoute>
                  <BillingPage />
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
              path="/proxacloud-tunnels"
              element={
                <ProtectedRoute requiredRoles={['super_user', 'super_admin']}>
                  <ProxacloudTunnelsPage />
                </ProtectedRoute>
              }
            />
            <Route
              path="/firewalls"
              element={
                <ProtectedRoute requiredRoles={['super_user', 'super_admin']}>
                  <FirewallsPage />
                </ProtectedRoute>
              }
            />
            <Route
              path="/monitoring"
              element={
                <ProtectedRoute requiredRoles={['super_user', 'super_admin']}>
                  <MonitoringPage />
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
              path="/regions"
              element={
                <ProtectedRoute requiredRoles={['super_user', 'super_admin']}>
                  <RegionsPage />
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
              path="/nodes"
              element={
                <ProtectedRoute>
                  <ProxmoxPage />
                </ProtectedRoute>
              }
            />
            <Route
              path="/storage"
              element={
                <ProtectedRoute requiredRoles={['super_user', 'super_admin']}>
                  <StoragePage />
                </ProtectedRoute>
              }
            />
            <Route
              path="/backups"
              element={
                <ProtectedRoute>
                  <BackupsPage />
                </ProtectedRoute>
              }
            />
            <Route path="/proxmox" element={<Navigate to="/nodes" replace />} />
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
              path="/image-pull-secrets"
              element={
                <ProtectedRoute requiredRoles={['super_user', 'super_admin']}>
                  <ClusterImagePullSecretsPage />
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
                <ProtectedRoute>
                  <DocumentsPage />
                </ProtectedRoute>
              }
            />
            <Route
              path="/documents/quick-commands"
              element={
                <ProtectedRoute>
                  <DocumentsPage category="quick-commands" />
                </ProtectedRoute>
              }
            />
            <Route
              path="/documents/database-cluster"
              element={
                <ProtectedRoute>
                  <DocumentsPage category="database-cluster" />
                </ProtectedRoute>
              }
            />
            <Route
              path="/documents/kubernetes-cluster"
              element={
                <ProtectedRoute>
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
