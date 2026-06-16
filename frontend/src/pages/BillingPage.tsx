import { useEffect, useMemo, useState, type FormEvent } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useSearchParams } from 'react-router-dom';
import { CreditCard, Database, Monitor, RefreshCw, Server, WalletCards } from 'lucide-react';
import {
  addBillingCredit,
  capturePayPalPayment,
  chargeCurrentMonth,
  createBillingCheckout,
  getBillingAccounts,
  getBillingSummary,
  updateBillingPrice,
  type BillingPrice,
} from '../api/billing';
import { Layout, PageHeader } from '../components/Layout';
import { Card, CardContent, CardHeader, CardTitle } from '../components/ui/Card';
import { Alert, ErrorMessage } from '../components/ui/Alert';
import { Button } from '../components/ui/Button';
import { Input, Select } from '../components/ui/Input';
import { Badge } from '../components/ui/Badge';
import { PageSpinner } from '../components/ui/Spinner';
import { useAuth } from '../context/AuthContext';

const resourceIcons: Record<string, typeof Monitor> = {
  vps: Monitor,
  container: Server,
  cluster: Server,
  database_cluster: Database,
  backup_storage: WalletCards,
};

function money(cents: number, currency = 'usd') {
  return new Intl.NumberFormat(undefined, { style: 'currency', currency: currency.toUpperCase() }).format((cents || 0) / 100);
}

export function BillingPage() {
  const { user } = useAuth();
  const isAdmin = user?.role === 'super_user' || user?.role === 'super_admin';
  const qc = useQueryClient();
  const [params, setParams] = useSearchParams();
  const [selectedAccountID, setSelectedAccountID] = useState<number | undefined>();
  const [topUpAmount, setTopUpAmount] = useState('25');
  const [creditAmount, setCreditAmount] = useState('10');
  const [creditDescription, setCreditDescription] = useState('Admin credit');
  const [editingPrice, setEditingPrice] = useState<BillingPrice | null>(null);

  const accountsQuery = useQuery({ queryKey: ['billing-accounts'], queryFn: getBillingAccounts, enabled: isAdmin });
  const summaryQuery = useQuery({
    queryKey: ['billing-summary', selectedAccountID],
    queryFn: () => getBillingSummary(selectedAccountID),
  });

  const summary = summaryQuery.data;
  const effectiveAccountID = selectedAccountID ?? summary?.account_id;

  useEffect(() => {
    if (!isAdmin || selectedAccountID || !accountsQuery.data?.length) return;
    setSelectedAccountID(accountsQuery.data[0].account_id);
  }, [accountsQuery.data, isAdmin, selectedAccountID]);

  const captureMut = useMutation({
    mutationFn: capturePayPalPayment,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['billing-summary'] });
      params.delete('transaction_id');
      params.delete('provider');
      params.set('payment', 'success');
      setParams(params, { replace: true });
    },
  });

  useEffect(() => {
    const provider = params.get('provider');
    const rawID = Number(params.get('transaction_id'));
    if (provider === 'paypal' && rawID > 0 && !captureMut.isPending && !captureMut.isSuccess) {
      captureMut.mutate(rawID);
    }
  }, [captureMut, params]);

  const checkoutMut = useMutation({
    mutationFn: (provider: 'stripe' | 'paypal') =>
      createBillingCheckout({ provider, amount_cents: Math.round(Number(topUpAmount || 0) * 100), currency: summary?.currency ?? 'usd' }, isAdmin ? effectiveAccountID : undefined),
    onSuccess: (resp) => {
      window.location.href = resp.checkout_url;
    },
  });

  const addCreditMut = useMutation({
    mutationFn: () =>
      addBillingCredit(effectiveAccountID ?? 0, {
        amount_cents: Math.round(Number(creditAmount || 0) * 100),
        description: creditDescription,
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['billing-summary'] });
      qc.invalidateQueries({ queryKey: ['billing-accounts'] });
    },
  });

  const chargeMut = useMutation({
    mutationFn: () => chargeCurrentMonth(effectiveAccountID ?? 0),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['billing-summary'] });
      qc.invalidateQueries({ queryKey: ['billing-accounts'] });
    },
  });

  const priceMut = useMutation({
    mutationFn: (price: BillingPrice) => updateBillingPrice(price.resource_type, price),
    onSuccess: () => {
      setEditingPrice(null);
      qc.invalidateQueries({ queryKey: ['billing-summary'] });
    },
  });

  const groupedCharges = useMemo(() => {
    const map = new Map<string, number>();
    for (const item of summary?.resource_charges ?? []) {
      map.set(item.resource_type, (map.get(item.resource_type) ?? 0) + item.amount_cents);
    }
    return Array.from(map.entries());
  }, [summary?.resource_charges]);

  const submitPrice = (event: FormEvent) => {
    event.preventDefault();
    if (editingPrice) priceMut.mutate(editingPrice);
  };

  return (
    <Layout>
      <PageHeader
        title="Billing"
        description="Account credits, monthly charges, and payment top-ups"
        actions={
          <Button variant="secondary" onClick={() => summaryQuery.refetch()}>
            <RefreshCw className="h-4 w-4" /> Refresh
          </Button>
        }
      />

      {params.get('payment') === 'success' && <Alert variant="success">Payment completed. Your account credit will be visible after the provider confirmation is processed.</Alert>}
      {params.get('payment') === 'cancelled' && <Alert variant="warning">Payment checkout was cancelled.</Alert>}
      {captureMut.error && <ErrorMessage error={captureMut.error} />}

      {summaryQuery.isLoading ? (
        <PageSpinner />
      ) : summaryQuery.error ? (
        <ErrorMessage error={summaryQuery.error} />
      ) : summary ? (
        <div className="space-y-6">
          {isAdmin && (
            <Card>
              <CardContent className="grid gap-4 md:grid-cols-3">
                <Select label="Customer account" value={effectiveAccountID ?? ''} onChange={(e) => setSelectedAccountID(Number(e.target.value))}>
                  {(accountsQuery.data ?? []).map((account) => (
                    <option key={account.account_id} value={account.account_id}>
                      {account.account_name} ({account.owner_username})
                    </option>
                  ))}
                </Select>
                <Input label="Credit amount" type="number" min="1" step="0.01" value={creditAmount} onChange={(e) => setCreditAmount(e.target.value)} />
                <div className="flex items-end gap-2">
                  <Input label="Description" value={creditDescription} onChange={(e) => setCreditDescription(e.target.value)} />
                  <Button onClick={() => addCreditMut.mutate()} loading={addCreditMut.isPending} disabled={!effectiveAccountID}>
                    Add Credit
                  </Button>
                </div>
                <div className="md:col-span-3">
                  {addCreditMut.error && <ErrorMessage error={addCreditMut.error} />}
                </div>
              </CardContent>
            </Card>
          )}

          <div className="grid gap-4 md:grid-cols-3">
            <Card>
              <CardContent className="flex items-center gap-4">
                <div className="flex h-10 w-10 items-center justify-center rounded-md bg-emerald-50 text-emerald-700">
                  <WalletCards className="h-5 w-5" />
                </div>
                <div>
                  <p className="text-sm font-medium text-slate-500">Credit Balance</p>
                  <p className="mt-1 text-2xl font-semibold text-slate-900">{money(summary.balance_cents, summary.currency)}</p>
                </div>
              </CardContent>
            </Card>
            <Card>
              <CardContent className="flex items-center gap-4">
                <div className="flex h-10 w-10 items-center justify-center rounded-md bg-sky-50 text-sky-700">
                  <CreditCard className="h-5 w-5" />
                </div>
                <div>
                  <p className="text-sm font-medium text-slate-500">Monthly Estimate</p>
                  <p className="mt-1 text-2xl font-semibold text-slate-900">{money(summary.estimated_monthly_cents, summary.currency)}</p>
                </div>
              </CardContent>
            </Card>
            <Card>
              <CardContent>
                <p className="text-sm font-medium text-slate-500">Billing Period</p>
                <p className="mt-1 text-lg font-semibold text-slate-900">{summary.current_billing_period.start}</p>
                <p className="text-sm text-slate-500">to {summary.current_billing_period.end}</p>
              </CardContent>
            </Card>
          </div>

          <Card>
            <CardHeader>
              <CardTitle>Add Funds</CardTitle>
            </CardHeader>
            <CardContent>
              <div className="grid gap-4 md:grid-cols-[200px_1fr]">
                <Input label="Amount" type="number" min="1" step="0.01" value={topUpAmount} onChange={(e) => setTopUpAmount(e.target.value)} />
                <div className="flex items-end gap-3">
                  <Button onClick={() => checkoutMut.mutate('stripe')} loading={checkoutMut.isPending && checkoutMut.variables === 'stripe'} disabled={!summary.providers.stripe}>
                    Stripe
                  </Button>
                  <Button variant="secondary" onClick={() => checkoutMut.mutate('paypal')} loading={checkoutMut.isPending && checkoutMut.variables === 'paypal'} disabled={!summary.providers.paypal}>
                    PayPal
                  </Button>
                </div>
              </div>
              {!summary.providers.stripe || !summary.providers.paypal ? (
                <p className="mt-3 text-xs text-slate-500">
                  Disabled providers are missing API credentials in the backend environment.
                </p>
              ) : null}
              {checkoutMut.error && <div className="mt-3"><ErrorMessage error={checkoutMut.error} /></div>}
            </CardContent>
          </Card>

          {isAdmin && (
            <Card>
              <CardHeader>
                <CardTitle>Post Monthly Charge</CardTitle>
              </CardHeader>
              <CardContent className="flex flex-wrap items-center justify-between gap-3">
                <p className="text-sm text-slate-600">
                  Posts this month&apos;s current resource estimate to the selected account ledger. Re-running updates the same month instead of duplicating it.
                </p>
                <Button variant="danger" onClick={() => chargeMut.mutate()} loading={chargeMut.isPending} disabled={!effectiveAccountID}>
                  Charge Current Month
                </Button>
                {chargeMut.error && <ErrorMessage error={chargeMut.error} />}
              </CardContent>
            </Card>
          )}

          <div className="grid gap-4 md:grid-cols-2">
            <Card>
              <CardHeader>
                <CardTitle>Resource Charges</CardTitle>
              </CardHeader>
              <CardContent>
                <div className="space-y-3">
                  {groupedCharges.map(([type, amount]) => {
                    const Icon = resourceIcons[type] ?? Server;
                    return (
                      <div key={type} className="flex items-center justify-between rounded-md border border-slate-200 px-4 py-3">
                        <div className="flex items-center gap-3">
                          <Icon className="h-5 w-5 text-slate-500" />
                          <span className="text-sm font-medium capitalize text-slate-800">{type.replace(/_/g, ' ')}</span>
                        </div>
                        <span className="text-sm font-semibold text-slate-900">{money(amount, summary.currency)}</span>
                      </div>
                    );
                  })}
                  {summary.resource_charges.length === 0 && <p className="text-sm text-slate-500">No owned billable resources yet.</p>}
                </div>
              </CardContent>
            </Card>

            <Card>
              <CardHeader>
                <CardTitle>Ledger</CardTitle>
              </CardHeader>
              <CardContent>
                <div className="space-y-3">
                  {summary.recent_ledger_entries.map((entry) => (
                    <div key={entry.id} className="flex items-center justify-between gap-3 border-b border-slate-100 pb-3 last:border-b-0 last:pb-0">
                      <div>
                        <p className="text-sm font-medium text-slate-900">{entry.description || entry.entry_type}</p>
                        <p className="text-xs text-slate-500">{new Date(entry.created_at).toLocaleString()}</p>
                      </div>
                      <span className={entry.amount_cents >= 0 ? 'text-sm font-semibold text-emerald-700' : 'text-sm font-semibold text-rose-700'}>
                        {money(entry.amount_cents, entry.currency)}
                      </span>
                    </div>
                  ))}
                  {summary.recent_ledger_entries.length === 0 && <p className="text-sm text-slate-500">No ledger entries yet.</p>}
                </div>
              </CardContent>
            </Card>
          </div>

          <Card>
            <CardHeader>
              <CardTitle>Detailed Monthly Items</CardTitle>
            </CardHeader>
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-slate-200 bg-slate-50">
                    <th className="px-6 py-3 text-left font-medium text-slate-600">Resource</th>
                    <th className="px-6 py-3 text-left font-medium text-slate-600">Type</th>
                    <th className="px-6 py-3 text-left font-medium text-slate-600">Size</th>
                    <th className="px-6 py-3 text-right font-medium text-slate-600">Monthly</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-slate-100">
                  {summary.resource_charges.map((item) => (
                    <tr key={`${item.resource_type}-${item.resource_id}`}>
                      <td className="px-6 py-3 font-medium text-slate-900">{item.resource_name}</td>
                      <td className="px-6 py-3"><Badge>{item.resource_type.replace(/_/g, ' ')}</Badge></td>
                      <td className="px-6 py-3 text-slate-600">{item.cpu} vCPU / {Math.ceil(item.memory_mb / 1024)} GB RAM / {item.disk_gb} GB disk</td>
                      <td className="px-6 py-3 text-right font-semibold text-slate-900">{money(item.amount_cents, summary.currency)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </Card>

          {isAdmin && (
            <Card>
              <CardHeader>
                <CardTitle>Monthly Price Rules</CardTitle>
              </CardHeader>
              <div className="overflow-x-auto">
                <table className="w-full text-sm">
                  <thead>
                    <tr className="border-b border-slate-200 bg-slate-50">
                      <th className="px-6 py-3 text-left font-medium text-slate-600">Resource</th>
                      <th className="px-6 py-3 text-right font-medium text-slate-600">Base</th>
                      <th className="px-6 py-3 text-right font-medium text-slate-600">vCPU</th>
                      <th className="px-6 py-3 text-right font-medium text-slate-600">RAM GB</th>
                      <th className="px-6 py-3 text-right font-medium text-slate-600">Disk GB</th>
                      <th className="px-6 py-3" />
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-slate-100">
                    {summary.prices.map((price) => (
                      <tr key={price.resource_type}>
                        <td className="px-6 py-3 font-medium text-slate-900">{price.display_name}</td>
                        <td className="px-6 py-3 text-right">{money(price.base_monthly_cents)}</td>
                        <td className="px-6 py-3 text-right">{money(price.cpu_monthly_cents)}</td>
                        <td className="px-6 py-3 text-right">{money(price.memory_gb_monthly_cents)}</td>
                        <td className="px-6 py-3 text-right">{money(price.disk_gb_monthly_cents)}</td>
                        <td className="px-6 py-3 text-right">
                          <Button variant="secondary" size="sm" onClick={() => setEditingPrice(price)}>Edit</Button>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </Card>
          )}

          {editingPrice && (
            <Card>
              <CardHeader>
                <CardTitle>Edit {editingPrice.display_name}</CardTitle>
              </CardHeader>
              <CardContent>
                <form onSubmit={submitPrice} className="grid gap-4 md:grid-cols-5">
                  <Input label="Name" value={editingPrice.display_name} onChange={(e) => setEditingPrice({ ...editingPrice, display_name: e.target.value })} />
                  <Input label="Base cents" type="number" value={editingPrice.base_monthly_cents} onChange={(e) => setEditingPrice({ ...editingPrice, base_monthly_cents: Number(e.target.value) })} />
                  <Input label="vCPU cents" type="number" value={editingPrice.cpu_monthly_cents} onChange={(e) => setEditingPrice({ ...editingPrice, cpu_monthly_cents: Number(e.target.value) })} />
                  <Input label="RAM GB cents" type="number" value={editingPrice.memory_gb_monthly_cents} onChange={(e) => setEditingPrice({ ...editingPrice, memory_gb_monthly_cents: Number(e.target.value) })} />
                  <Input label="Disk GB cents" type="number" value={editingPrice.disk_gb_monthly_cents} onChange={(e) => setEditingPrice({ ...editingPrice, disk_gb_monthly_cents: Number(e.target.value) })} />
                  <div className="md:col-span-5 flex gap-3">
                    <Button type="submit" loading={priceMut.isPending}>Save Price</Button>
                    <Button type="button" variant="secondary" onClick={() => setEditingPrice(null)}>Cancel</Button>
                  </div>
                  {priceMut.error && <div className="md:col-span-5"><ErrorMessage error={priceMut.error} /></div>}
                </form>
              </CardContent>
            </Card>
          )}
        </div>
      ) : null}
    </Layout>
  );
}
