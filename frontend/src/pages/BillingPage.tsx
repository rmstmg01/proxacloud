import { CreditCard, Database, Monitor, Server, WalletCards } from 'lucide-react';
import { Layout, PageHeader } from '../components/Layout';
import { Card, CardContent, CardHeader, CardTitle } from '../components/ui/Card';
import { Alert } from '../components/ui/Alert';
import { useAuth } from '../context/AuthContext';

const plannedMeters = [
  { label: 'VPS monthly plans', icon: Monitor },
  { label: 'Container monthly plans', icon: Server },
  { label: 'Cluster monthly plans', icon: Server },
  { label: 'Database monthly plans', icon: Database },
];

export function BillingPage() {
  const { user } = useAuth();

  return (
    <Layout>
      <PageHeader
        title="Billing"
        description="Account credits, monthly charges, and resource usage"
      />

      <div className="space-y-6">
        <Alert variant="info">
          Billing access is enabled for this account. Credit balance, invoices, and monthly resource charges will appear here once the billing engine is connected.
        </Alert>

        <div className="grid gap-4 md:grid-cols-3">
          <Card>
            <CardContent className="flex items-center gap-4">
              <div className="flex h-10 w-10 items-center justify-center rounded-md bg-emerald-50 text-emerald-700">
                <WalletCards className="h-5 w-5" />
              </div>
              <div>
                <p className="text-sm font-medium text-slate-500">Credit Balance</p>
                <p className="mt-1 text-2xl font-semibold text-slate-900">Pending</p>
              </div>
            </CardContent>
          </Card>

          <Card>
            <CardContent className="flex items-center gap-4">
              <div className="flex h-10 w-10 items-center justify-center rounded-md bg-sky-50 text-sky-700">
                <CreditCard className="h-5 w-5" />
              </div>
              <div>
                <p className="text-sm font-medium text-slate-500">Billing Mode</p>
                <p className="mt-1 text-2xl font-semibold text-slate-900">Monthly</p>
              </div>
            </CardContent>
          </Card>

          <Card>
            <CardContent>
              <p className="text-sm font-medium text-slate-500">Account</p>
              <p className="mt-1 truncate text-2xl font-semibold text-slate-900">{user?.username}</p>
            </CardContent>
          </Card>
        </div>

        <Card>
          <CardHeader>
            <CardTitle>Planned Monthly Charges</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="grid gap-3 md:grid-cols-2">
              {plannedMeters.map(({ label, icon: Icon }) => (
                <div key={label} className="flex items-center gap-3 rounded-md border border-slate-200 px-4 py-3">
                  <Icon className="h-5 w-5 text-slate-500" />
                  <span className="text-sm font-medium text-slate-800">{label}</span>
                </div>
              ))}
            </div>
          </CardContent>
        </Card>
      </div>
    </Layout>
  );
}
