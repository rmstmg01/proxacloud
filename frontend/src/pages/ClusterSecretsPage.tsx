import { Link } from 'react-router-dom';
import { KeyRound, ChevronRight } from 'lucide-react';
import { useQuery } from '@tanstack/react-query';
import { getClusters } from '../api/clusters';
import { Layout, PageHeader } from '../components/Layout';
import { Card, CardContent } from '../components/ui/Card';
import { PageSpinner } from '../components/ui/Spinner';
import { ErrorMessage } from '../components/ui/Alert';

export function ClusterSecretsPage() {
  const { data: clusters, isLoading, error } = useQuery({ queryKey: ['clusters'], queryFn: getClusters });

  return (
    <Layout>
      <PageHeader title="Secrets" description="Manage cluster-scoped and global application secrets" />
      {isLoading && <PageSpinner />}
      {error && <ErrorMessage error={error} />}
      {clusters && (
        <Card>
          <CardContent>
            {clusters.length === 0 ? (
              <p className="text-sm text-slate-400">No clusters available.</p>
            ) : (
              <div className="divide-y divide-slate-100">
                {clusters.map((cluster) => (
                  <Link
                    key={cluster.id}
                    to={`/clusters/${cluster.id}?tab=secrets`}
                    className="flex items-center justify-between gap-4 py-4 hover:bg-slate-50 -mx-6 px-6 transition-colors"
                  >
                    <div className="flex items-center gap-3">
                      <div className="h-9 w-9 rounded-md bg-indigo-50 text-indigo-600 flex items-center justify-center">
                        <KeyRound className="h-4 w-4" />
                      </div>
                      <div>
                        <p className="text-sm font-medium text-slate-900">{cluster.name}</p>
                        <p className="text-xs text-slate-500">{cluster.topology_type} · {cluster.kubernetes_version}</p>
                      </div>
                    </div>
                    <span className="inline-flex items-center gap-1 text-sm text-indigo-600">
                      Manage secrets <ChevronRight className="h-4 w-4" />
                    </span>
                  </Link>
                ))}
              </div>
            )}
          </CardContent>
        </Card>
      )}
    </Layout>
  );
}
