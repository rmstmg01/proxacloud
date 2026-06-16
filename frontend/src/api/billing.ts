import client from './client';

export type BillingPrice = {
  id: number;
  resource_type: string;
  display_name: string;
  base_monthly_cents: number;
  cpu_monthly_cents: number;
  memory_gb_monthly_cents: number;
  disk_gb_monthly_cents: number;
  enabled: boolean;
  updated_at: string;
};

export type ResourceCharge = {
  resource_type: string;
  resource_id: number;
  resource_name: string;
  cpu: number;
  memory_mb: number;
  disk_gb: number;
  amount_cents: number;
  description: string;
};

export type LedgerEntry = {
  id: number;
  amount_cents: number;
  currency: string;
  entry_type: string;
  description: string;
  source: string;
  source_id: string;
  created_at: string;
};

export type PaymentRecord = {
  id: number;
  provider: string;
  provider_session_id: string;
  amount_cents: number;
  currency: string;
  status: string;
  checkout_url?: string;
  created_at: string;
  updated_at: string;
};

export type BillingAccount = {
  account_id: number;
  account_name: string;
  account_status: string;
  owner_username: string;
  owner_email: string;
  balance_cents: number;
  estimated_monthly_cents: number;
  currency: string;
};

export type BillingSummary = BillingAccount & {
  resource_charges: ResourceCharge[];
  recent_ledger_entries: LedgerEntry[];
  recent_payments: PaymentRecord[];
  prices: BillingPrice[];
  providers: { stripe: boolean; paypal: boolean };
  current_billing_period: { start: string; end: string };
};

export const getBillingSummary = (accountID?: number) => {
  const query = accountID ? `?account_id=${accountID}` : '';
  return client.get<BillingSummary>(`/billing/summary${query}`).then((r) => r.data);
};

export const getBillingAccounts = () =>
  client.get<BillingAccount[]>('/billing/accounts').then((r) => r.data);

export const createBillingCheckout = (body: { provider: 'stripe' | 'paypal'; amount_cents: number; currency?: string }, accountID?: number) => {
  const query = accountID ? `?account_id=${accountID}` : '';
  return client
    .post<{ transaction_id: number; provider: string; checkout_url: string; status: string }>(`/billing/checkout${query}`, body)
    .then((r) => r.data);
};

export const capturePayPalPayment = (transactionID: number) =>
  client.post<PaymentRecord>(`/billing/paypal/${transactionID}/capture`).then((r) => r.data);

export const addBillingCredit = (accountID: number, body: { amount_cents: number; description: string }) =>
  client.post<LedgerEntry>(`/billing/accounts/${accountID}/credits`, body).then((r) => r.data);

export const chargeCurrentMonth = (accountID: number) =>
  client.post<BillingSummary>(`/billing/accounts/${accountID}/charge-current-month`).then((r) => r.data);

export const updateBillingPrice = (resourceType: string, body: BillingPrice) =>
  client.put<BillingPrice>(`/billing/prices/${resourceType}`, body).then((r) => r.data);
