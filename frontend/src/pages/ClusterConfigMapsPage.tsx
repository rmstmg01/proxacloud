import { Link } from 'react-router-dom';
import { FileText, ChevronRight } from 'lucide-react';
import { useQuery } from '@tanstack/react-query';
import { getClusters } from '../api/clusters';
import { Layout, PageHeader } from '../components/Layout';
import { Card, CardContent } from '../components/ui/Card';
import { PageSpinner } from '../components/ui/Spinner';
import { ErrorMessage } from '../components/ui/Alert';

export function ClusterConfigMapsPage() {
  const { data: clusters, isLoading, error } = useQuery({ queryKey: ['clusters'], queryFn: getClusters });

  return (
    <Layout>
      <PageHeader title="ConfigMaps" description="Manage reusable non-sensitive application configuration" />
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
                    to={`/clusters/${cluster.id}?tab=configmaps`}
                    className="flex items-center justify-between gap-4 py-4 hover:bg-slate-50 -mx-6 px-6 transition-colors"
                  >
                    <div className="flex items-center gap-3">
                      <div className="h-9 w-9 rounded-md bg-emerald-50 text-emerald-600 flex items-center justify-center">
                        <FileText className="h-4 w-4" />
                      </div>
                      <div>
                        <p className="text-sm font-medium text-slate-900">{cluster.name}</p>
                        <p className="text-xs text-slate-500">{cluster.topology_type} · {cluster.kubernetes_version}</p>
                      </div>
                    </div>
                    <span className="inline-flex items-center gap-1 text-sm text-indigo-600">
                      Manage ConfigMaps <ChevronRight className="h-4 w-4" />
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
