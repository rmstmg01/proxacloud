package billing

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"proxacloud/backend/internal/rbac"
	"proxacloud/backend/internal/tenant"
)

const defaultCurrency = "usd"

var (
	ErrAccountNotFound     = errors.New("billing account not found")
	ErrInvalidAmount       = errors.New("amount must be greater than zero")
	ErrInvalidProvider     = errors.New("unsupported payment provider")
	ErrTransactionNotFound = errors.New("payment transaction not found")
)

type ScopeResolver interface {
	TenantScope(username string, role rbac.Role) (tenant.Scope, error)
}

type Config struct {
	PublicAppURL        string
	StripeSecretKey     string
	StripeWebhookSecret string
	PayPalClientID      string
	PayPalClientSecret  string
	PayPalEnvironment   string
}

type Service struct {
	db     *sql.DB
	cfg    Config
	stripe *StripeClient
	paypal *PayPalClient
}

type Price struct {
	ID                   int64     `json:"id"`
	ResourceType         string    `json:"resource_type"`
	DisplayName          string    `json:"display_name"`
	BaseMonthlyCents     int       `json:"base_monthly_cents"`
	CPUMonthlyCents      int       `json:"cpu_monthly_cents"`
	MemoryGBMonthlyCents int       `json:"memory_gb_monthly_cents"`
	DiskGBMonthlyCents   int       `json:"disk_gb_monthly_cents"`
	Enabled              bool      `json:"enabled"`
	UpdatedAt            time.Time `json:"updated_at"`
}

type AccountSummary struct {
	AccountID             int64               `json:"account_id"`
	AccountName           string              `json:"account_name"`
	AccountStatus         string              `json:"account_status"`
	OwnerUsername         string              `json:"owner_username"`
	OwnerEmail            string              `json:"owner_email"`
	BalanceCents          int                 `json:"balance_cents"`
	Currency              string              `json:"currency"`
	EstimatedMonthlyCents int                 `json:"estimated_monthly_cents"`
	ResourceCharges       []ResourceCharge    `json:"resource_charges"`
	RecentLedgerEntries   []LedgerEntry       `json:"recent_ledger_entries"`
	RecentPayments        []PaymentRecord     `json:"recent_payments"`
	Prices                []Price             `json:"prices"`
	Providers             PaymentProviderInfo `json:"providers"`
	CurrentBillingPeriod  BillingPeriod       `json:"current_billing_period"`
}

type AccountListItem struct {
	AccountID             int64  `json:"account_id"`
	AccountName           string `json:"account_name"`
	AccountStatus         string `json:"account_status"`
	OwnerUsername         string `json:"owner_username"`
	OwnerEmail            string `json:"owner_email"`
	BalanceCents          int    `json:"balance_cents"`
	EstimatedMonthlyCents int    `json:"estimated_monthly_cents"`
	Currency              string `json:"currency"`
}

type PaymentProviderInfo struct {
	Stripe bool `json:"stripe"`
	PayPal bool `json:"paypal"`
}

type BillingPeriod struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

type ResourceCharge struct {
	ResourceType string `json:"resource_type"`
	ResourceID   int64  `json:"resource_id"`
	ResourceName string `json:"resource_name"`
	CPU          int    `json:"cpu"`
	MemoryMB     int    `json:"memory_mb"`
	DiskGB       int    `json:"disk_gb"`
	AmountCents  int    `json:"amount_cents"`
	Description  string `json:"description"`
}

type LedgerEntry struct {
	ID          int64     `json:"id"`
	AmountCents int       `json:"amount_cents"`
	Currency    string    `json:"currency"`
	EntryType   string    `json:"entry_type"`
	Description string    `json:"description"`
	Source      string    `json:"source"`
	SourceID    string    `json:"source_id"`
	CreatedAt   time.Time `json:"created_at"`
}

type PaymentRecord struct {
	ID                int64     `json:"id"`
	Provider          string    `json:"provider"`
	ProviderSessionID string    `json:"provider_session_id"`
	AmountCents       int       `json:"amount_cents"`
	Currency          string    `json:"currency"`
	Status            string    `json:"status"`
	CheckoutURL       string    `json:"checkout_url,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type CheckoutRequest struct {
	Provider    string `json:"provider"`
	AmountCents int    `json:"amount_cents"`
	Currency    string `json:"currency"`
}

type CheckoutResponse struct {
	TransactionID int64  `json:"transaction_id"`
	Provider      string `json:"provider"`
	CheckoutURL   string `json:"checkout_url"`
	Status        string `json:"status"`
}

func NewService(db *sql.DB, cfg Config) *Service {
	return &Service{
		db:     db,
		cfg:    cfg,
		stripe: NewStripeClient(cfg.StripeSecretKey, cfg.StripeWebhookSecret),
		paypal: NewPayPalClient(cfg.PayPalClientID, cfg.PayPalClientSecret, cfg.PayPalEnvironment),
	}
}

func (s *Service) Providers() PaymentProviderInfo {
	return PaymentProviderInfo{Stripe: s.stripe.Enabled(), PayPal: s.paypal.Enabled()}
}

func (s *Service) Summary(scope tenant.Scope, accountID int64) (AccountSummary, error) {
	resolvedID, err := s.resolveAccountID(scope, accountID)
	if err != nil {
		return AccountSummary{}, err
	}
	account, err := s.accountInfo(resolvedID)
	if err != nil {
		return AccountSummary{}, err
	}
	prices, err := s.ListPrices()
	if err != nil {
		return AccountSummary{}, err
	}
	charges, total, err := s.EstimateMonthlyCharges(resolvedID)
	if err != nil {
		return AccountSummary{}, err
	}
	balance, err := s.Balance(resolvedID)
	if err != nil {
		return AccountSummary{}, err
	}
	ledger, err := s.ListLedger(resolvedID, 25)
	if err != nil {
		return AccountSummary{}, err
	}
	payments, err := s.ListPayments(resolvedID, 10)
	if err != nil {
		return AccountSummary{}, err
	}
	start, end := currentBillingPeriod()
	return AccountSummary{
		AccountID:             resolvedID,
		AccountName:           account.Name,
		AccountStatus:         account.Status,
		OwnerUsername:         account.OwnerUsername,
		OwnerEmail:            account.OwnerEmail,
		BalanceCents:          balance,
		Currency:              defaultCurrency,
		EstimatedMonthlyCents: total,
		ResourceCharges:       charges,
		RecentLedgerEntries:   ledger,
		RecentPayments:        payments,
		Prices:                prices,
		Providers:             s.Providers(),
		CurrentBillingPeriod:  BillingPeriod{Start: start.Format("2006-01-02"), End: end.Format("2006-01-02")},
	}, nil
}

func (s *Service) ListAccounts() ([]AccountListItem, error) {
	rows, err := s.db.Query(`
		SELECT ca.id, ca.name, ca.status, u.username, u.email
		FROM customer_accounts ca
		JOIN users u ON u.id = ca.owner_user_id
		ORDER BY ca.created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list billing accounts: %w", err)
	}
	defer rows.Close()
	out := []AccountListItem{}
	for rows.Next() {
		var item AccountListItem
		if err := rows.Scan(&item.AccountID, &item.AccountName, &item.AccountStatus, &item.OwnerUsername, &item.OwnerEmail); err != nil {
			return nil, err
		}
		item.Currency = defaultCurrency
		item.BalanceCents, _ = s.Balance(item.AccountID)
		_, item.EstimatedMonthlyCents, _ = s.EstimateMonthlyCharges(item.AccountID)
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Service) ListPrices() ([]Price, error) {
	rows, err := s.db.Query(`SELECT id, resource_type, display_name, base_monthly_cents, cpu_monthly_cents, memory_gb_monthly_cents, disk_gb_monthly_cents, enabled, updated_at FROM billing_prices ORDER BY id ASC`)
	if err != nil {
		return nil, fmt.Errorf("list billing prices: %w", err)
	}
	defer rows.Close()
	out := []Price{}
	for rows.Next() {
		var p Price
		if err := rows.Scan(&p.ID, &p.ResourceType, &p.DisplayName, &p.BaseMonthlyCents, &p.CPUMonthlyCents, &p.MemoryGBMonthlyCents, &p.DiskGBMonthlyCents, &p.Enabled, &p.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Service) UpdatePrice(resourceType string, p Price) (Price, error) {
	resourceType = strings.TrimSpace(resourceType)
	if resourceType == "" {
		return Price{}, errors.New("resource_type is required")
	}
	err := s.db.QueryRow(`
		UPDATE billing_prices
		   SET display_name = $2,
		       base_monthly_cents = $3,
		       cpu_monthly_cents = $4,
		       memory_gb_monthly_cents = $5,
		       disk_gb_monthly_cents = $6,
		       enabled = $7,
		       updated_at = NOW()
		 WHERE resource_type = $1
		 RETURNING id, resource_type, display_name, base_monthly_cents, cpu_monthly_cents, memory_gb_monthly_cents, disk_gb_monthly_cents, enabled, updated_at`,
		resourceType, strings.TrimSpace(p.DisplayName), p.BaseMonthlyCents, p.CPUMonthlyCents, p.MemoryGBMonthlyCents, p.DiskGBMonthlyCents, p.Enabled,
	).Scan(&p.ID, &p.ResourceType, &p.DisplayName, &p.BaseMonthlyCents, &p.CPUMonthlyCents, &p.MemoryGBMonthlyCents, &p.DiskGBMonthlyCents, &p.Enabled, &p.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Price{}, errors.New("billing price not found")
	}
	return p, err
}

func (s *Service) Balance(accountID int64) (int, error) {
	var balance sql.NullInt64
	err := s.db.QueryRow(`SELECT COALESCE(SUM(amount_cents), 0) FROM account_credit_ledger WHERE account_id = $1`, accountID).Scan(&balance)
	return int(balance.Int64), err
}

func (s *Service) AddCredit(accountID int64, amountCents int, description, source, sourceID string, actorID int64, idempotencyKey string) (LedgerEntry, error) {
	if amountCents <= 0 {
		return LedgerEntry{}, ErrInvalidAmount
	}
	return s.insertLedger(accountID, amountCents, "credit", description, source, sourceID, actorID, idempotencyKey)
}

func (s *Service) ChargeCurrentMonth(accountID int64) (AccountSummary, error) {
	if _, err := s.accountInfo(accountID); err != nil {
		return AccountSummary{}, err
	}
	charges, total, err := s.EstimateMonthlyCharges(accountID)
	if err != nil {
		return AccountSummary{}, err
	}
	start, end := currentBillingPeriod()
	tx, err := s.db.Begin()
	if err != nil {
		return AccountSummary{}, err
	}
	defer tx.Rollback()
	var invoiceID int64
	err = tx.QueryRow(`
		INSERT INTO billing_invoices (account_id, period_start, period_end, subtotal_cents, currency, source)
		VALUES ($1, $2, $3, $4, $5, 'monthly_resource_charge')
		ON CONFLICT (account_id, period_start, source)
		DO UPDATE SET period_end = EXCLUDED.period_end, subtotal_cents = EXCLUDED.subtotal_cents, updated_at = NOW()
		RETURNING id`, accountID, start, end, total, defaultCurrency).Scan(&invoiceID)
	if err != nil {
		return AccountSummary{}, fmt.Errorf("upsert invoice: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM billing_invoice_items WHERE invoice_id = $1`, invoiceID); err != nil {
		return AccountSummary{}, err
	}
	for _, item := range charges {
		if _, err := tx.Exec(`
			INSERT INTO billing_invoice_items (invoice_id, resource_type, resource_id, resource_name, quantity, amount_cents, description)
			VALUES ($1, $2, $3, $4, 1, $5, $6)`, invoiceID, item.ResourceType, item.ResourceID, item.ResourceName, item.AmountCents, item.Description); err != nil {
			return AccountSummary{}, err
		}
	}
	idempotencyKey := fmt.Sprintf("monthly:%s", start.Format("2006-01"))
	if total > 0 {
		if _, err := tx.Exec(`
			INSERT INTO account_credit_ledger (account_id, amount_cents, currency, entry_type, description, source, source_id, idempotency_key)
			VALUES ($1, $2, $3, 'debit', $4, 'invoice', $5, $6)
			ON CONFLICT (account_id, idempotency_key) WHERE idempotency_key <> ''
			DO UPDATE SET amount_cents = EXCLUDED.amount_cents, description = EXCLUDED.description, source_id = EXCLUDED.source_id`,
			accountID, -total, defaultCurrency, "Monthly resource charges "+start.Format("January 2006"), fmt.Sprint(invoiceID), idempotencyKey); err != nil {
			return AccountSummary{}, fmt.Errorf("post monthly charge: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return AccountSummary{}, err
	}
	return s.Summary(tenant.Scope{Role: rbac.RoleSuperAdmin}, accountID)
}

func (s *Service) CreateCheckout(accountID int64, req CheckoutRequest) (CheckoutResponse, error) {
	if req.AmountCents <= 0 {
		return CheckoutResponse{}, ErrInvalidAmount
	}
	currency := strings.ToLower(strings.TrimSpace(req.Currency))
	if currency == "" {
		currency = defaultCurrency
	}
	provider := strings.ToLower(strings.TrimSpace(req.Provider))
	if provider != "stripe" && provider != "paypal" {
		return CheckoutResponse{}, ErrInvalidProvider
	}
	var txID int64
	if err := s.db.QueryRow(`
		INSERT INTO payment_transactions (account_id, provider, amount_cents, currency, status)
		VALUES ($1, $2, $3, $4, 'pending') RETURNING id`, accountID, provider, req.AmountCents, currency).Scan(&txID); err != nil {
		return CheckoutResponse{}, err
	}
	successURL := strings.TrimRight(s.cfg.PublicAppURL, "/") + "/billing?payment=success"
	cancelURL := strings.TrimRight(s.cfg.PublicAppURL, "/") + "/billing?payment=cancelled"
	var checkoutURL, sessionID, raw string
	var err error
	switch provider {
	case "stripe":
		sessionID, checkoutURL, raw, err = s.stripe.CreateCheckoutSession(txID, req.AmountCents, currency, successURL, cancelURL)
	case "paypal":
		sessionID, checkoutURL, raw, err = s.paypal.CreateOrder(txID, req.AmountCents, currency, successURL, cancelURL)
	}
	if err != nil {
		_, _ = s.db.Exec(`UPDATE payment_transactions SET status = 'failed', raw_response = $2, updated_at = NOW() WHERE id = $1`, txID, err.Error())
		return CheckoutResponse{}, err
	}
	if _, err := s.db.Exec(`UPDATE payment_transactions SET provider_session_id = $2, checkout_url = $3, raw_response = $4, updated_at = NOW() WHERE id = $1`, txID, sessionID, checkoutURL, raw); err != nil {
		return CheckoutResponse{}, err
	}
	return CheckoutResponse{TransactionID: txID, Provider: provider, CheckoutURL: checkoutURL, Status: "pending"}, nil
}

func (s *Service) CapturePayPalOrder(accountID int64, transactionID int64) (PaymentRecord, error) {
	payment, err := s.Payment(transactionID)
	if err != nil {
		return PaymentRecord{}, err
	}
	if payment.AccountID != accountID {
		return PaymentRecord{}, ErrTransactionNotFound
	}
	raw, err := s.paypal.CaptureOrder(payment.ProviderSessionID)
	if err != nil {
		return PaymentRecord{}, err
	}
	if err := s.CompletePayment(transactionID, payment.ProviderSessionID, raw); err != nil {
		return PaymentRecord{}, err
	}
	completed, err := s.Payment(transactionID)
	if err != nil {
		return PaymentRecord{}, err
	}
	return completed.PaymentRecord, nil
}

func (s *Service) CompletePayment(transactionID int64, providerSessionID, raw string) error {
	payment, err := s.Payment(transactionID)
	if err != nil {
		return err
	}
	if payment.Status == "succeeded" {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE payment_transactions SET status = 'succeeded', provider_session_id = COALESCE(NULLIF($2, ''), provider_session_id), raw_response = $3, updated_at = NOW() WHERE id = $1`, transactionID, providerSessionID, raw); err != nil {
		return err
	}
	if _, err := tx.Exec(`
		INSERT INTO account_credit_ledger (account_id, amount_cents, currency, entry_type, description, source, source_id, idempotency_key)
		VALUES ($1, $2, $3, 'credit', $4, 'payment', $5, $6)
		ON CONFLICT (account_id, idempotency_key) WHERE idempotency_key <> '' DO NOTHING`,
		payment.AccountID, payment.AmountCents, payment.Currency, "Payment via "+payment.Provider, fmt.Sprint(transactionID), fmt.Sprintf("payment:%d", transactionID)); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) Payment(id int64) (PaymentRecordWithAccount, error) {
	var p PaymentRecordWithAccount
	err := s.db.QueryRow(`SELECT id, account_id, provider, provider_session_id, amount_cents, currency, status, checkout_url, created_at, updated_at FROM payment_transactions WHERE id = $1`, id).
		Scan(&p.ID, &p.AccountID, &p.Provider, &p.ProviderSessionID, &p.AmountCents, &p.Currency, &p.Status, &p.CheckoutURL, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return PaymentRecordWithAccount{}, ErrTransactionNotFound
	}
	return p, err
}

type PaymentRecordWithAccount struct {
	PaymentRecord
	AccountID int64 `json:"account_id"`
}

func (s *Service) EstimateMonthlyCharges(accountID int64) ([]ResourceCharge, int, error) {
	prices, err := s.priceMap()
	if err != nil {
		return nil, 0, err
	}
	out := []ResourceCharge{}
	total := 0
	addRows := func(resourceType, query string) error {
		price, ok := prices[resourceType]
		if !ok || !price.Enabled {
			return nil
		}
		rows, err := s.db.Query(query, accountID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var c ResourceCharge
			c.ResourceType = resourceType
			if err := rows.Scan(&c.ResourceID, &c.ResourceName, &c.CPU, &c.MemoryMB, &c.DiskGB); err != nil {
				return err
			}
			c.AmountCents = rate(price, c.CPU, c.MemoryMB, c.DiskGB)
			c.Description = describeCharge(price, c.CPU, c.MemoryMB, c.DiskGB)
			total += c.AmountCents
			out = append(out, c)
		}
		return rows.Err()
	}
	if err := addRows("vps", `SELECT id, name, cpu, memory_mb, disk_gb FROM vps_instances WHERE customer_account_id = $1 AND status NOT IN ('deleted')`); err != nil {
		return nil, 0, err
	}
	if err := addRows("container", `SELECT id, name, cpu, memory_mb, disk_gb FROM container_instances WHERE customer_account_id = $1 AND status NOT IN ('deleted')`); err != nil {
		return nil, 0, err
	}
	if err := addRows("cluster", `SELECT id, name, (control_plane_count * control_plane_cpu) + (worker_count * worker_cpu), (control_plane_count * control_plane_ram_mb) + (worker_count * worker_ram_mb), (control_plane_count * control_plane_disk_gb) + (worker_count * worker_disk_gb) FROM clusters WHERE customer_account_id = $1 AND status NOT IN ('deleted')`); err != nil {
		return nil, 0, err
	}
	if err := addRows("database_cluster", `SELECT dc.id, dc.name, COALESCE(SUM(dcn.cpu), 0)::INT, COALESCE(SUM(dcn.memory_mb), 0)::INT, COALESCE(SUM(dcn.disk_gb), 0)::INT FROM database_clusters dc LEFT JOIN database_cluster_nodes dcn ON dcn.database_cluster_id = dc.id WHERE dc.customer_account_id = $1 AND dc.status NOT IN ('deleted') GROUP BY dc.id, dc.name`); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

func (s *Service) insertLedger(accountID int64, amountCents int, entryType, description, source, sourceID string, actorID int64, idempotencyKey string) (LedgerEntry, error) {
	var actor any
	if actorID > 0 {
		actor = actorID
	}
	var entry LedgerEntry
	err := s.db.QueryRow(`
		INSERT INTO account_credit_ledger (account_id, amount_cents, currency, entry_type, description, source, source_id, created_by, idempotency_key)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, amount_cents, currency, entry_type, description, source, source_id, created_at`,
		accountID, amountCents, defaultCurrency, entryType, strings.TrimSpace(description), strings.TrimSpace(source), strings.TrimSpace(sourceID), actor, strings.TrimSpace(idempotencyKey),
	).Scan(&entry.ID, &entry.AmountCents, &entry.Currency, &entry.EntryType, &entry.Description, &entry.Source, &entry.SourceID, &entry.CreatedAt)
	return entry, err
}

func (s *Service) ListLedger(accountID int64, limit int) ([]LedgerEntry, error) {
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	rows, err := s.db.Query(`SELECT id, amount_cents, currency, entry_type, description, source, source_id, created_at FROM account_credit_ledger WHERE account_id = $1 ORDER BY created_at DESC, id DESC LIMIT $2`, accountID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LedgerEntry{}
	for rows.Next() {
		var e LedgerEntry
		if err := rows.Scan(&e.ID, &e.AmountCents, &e.Currency, &e.EntryType, &e.Description, &e.Source, &e.SourceID, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Service) ListPayments(accountID int64, limit int) ([]PaymentRecord, error) {
	if limit <= 0 || limit > 100 {
		limit = 10
	}
	rows, err := s.db.Query(`SELECT id, provider, provider_session_id, amount_cents, currency, status, checkout_url, created_at, updated_at FROM payment_transactions WHERE account_id = $1 ORDER BY created_at DESC, id DESC LIMIT $2`, accountID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PaymentRecord{}
	for rows.Next() {
		var p PaymentRecord
		if err := rows.Scan(&p.ID, &p.Provider, &p.ProviderSessionID, &p.AmountCents, &p.Currency, &p.Status, &p.CheckoutURL, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Service) resolveAccountID(scope tenant.Scope, requested int64) (int64, error) {
	if scope.IsCustomer() {
		return scope.CustomerAccountID, nil
	}
	if requested > 0 {
		return requested, nil
	}
	var id int64
	err := s.db.QueryRow(`SELECT id FROM customer_accounts ORDER BY created_at DESC LIMIT 1`).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrAccountNotFound
	}
	return id, err
}

type accountInfo struct {
	Name          string
	Status        string
	OwnerUsername string
	OwnerEmail    string
}

func (s *Service) accountInfo(accountID int64) (accountInfo, error) {
	var info accountInfo
	err := s.db.QueryRow(`
		SELECT ca.name, ca.status, u.username, u.email
		FROM customer_accounts ca
		JOIN users u ON u.id = ca.owner_user_id
		WHERE ca.id = $1`, accountID).Scan(&info.Name, &info.Status, &info.OwnerUsername, &info.OwnerEmail)
	if errors.Is(err, sql.ErrNoRows) {
		return accountInfo{}, ErrAccountNotFound
	}
	return info, err
}

func (s *Service) priceMap() (map[string]Price, error) {
	prices, err := s.ListPrices()
	if err != nil {
		return nil, err
	}
	out := make(map[string]Price, len(prices))
	for _, p := range prices {
		out[p.ResourceType] = p
	}
	return out, nil
}

func rate(p Price, cpu, memoryMB, diskGB int) int {
	memoryGB := int(math.Ceil(float64(memoryMB) / 1024.0))
	if memoryGB < 0 {
		memoryGB = 0
	}
	return p.BaseMonthlyCents + cpu*p.CPUMonthlyCents + memoryGB*p.MemoryGBMonthlyCents + diskGB*p.DiskGBMonthlyCents
}

func describeCharge(p Price, cpu, memoryMB, diskGB int) string {
	memoryGB := int(math.Ceil(float64(memoryMB) / 1024.0))
	return fmt.Sprintf("%s monthly: base + %d vCPU + %d GB RAM + %d GB disk", p.DisplayName, cpu, memoryGB, diskGB)
}

func currentBillingPeriod() (time.Time, time.Time) {
	now := time.Now().UTC()
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0).Add(-24 * time.Hour)
	return start, end
}

func toRaw(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
