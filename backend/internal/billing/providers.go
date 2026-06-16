package billing

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type StripeClient struct {
	secretKey     string
	webhookSecret string
	httpClient    *http.Client
}

func NewStripeClient(secretKey, webhookSecret string) *StripeClient {
	return &StripeClient{
		secretKey:     strings.TrimSpace(secretKey),
		webhookSecret: strings.TrimSpace(webhookSecret),
		httpClient:    &http.Client{Timeout: 20 * time.Second},
	}
}

func (c *StripeClient) Enabled() bool {
	return c.secretKey != ""
}

func (c *StripeClient) CreateCheckoutSession(transactionID int64, amountCents int, currency, successURL, cancelURL string) (string, string, string, error) {
	if !c.Enabled() {
		return "", "", "", errors.New("stripe is not configured")
	}
	form := url.Values{}
	form.Set("mode", "payment")
	form.Set("success_url", successURL)
	form.Set("cancel_url", cancelURL)
	form.Set("client_reference_id", fmt.Sprint(transactionID))
	form.Set("customer_creation", "if_required")
	form.Set("line_items[0][quantity]", "1")
	form.Set("line_items[0][price_data][currency]", strings.ToLower(currency))
	form.Set("line_items[0][price_data][unit_amount]", fmt.Sprint(amountCents))
	form.Set("line_items[0][price_data][product_data][name]", "ProxaCloud account credit")
	form.Set("metadata[transaction_id]", fmt.Sprint(transactionID))

	req, err := http.NewRequest(http.MethodPost, "https://api.stripe.com/v1/checkout/sessions", strings.NewReader(form.Encode()))
	if err != nil {
		return "", "", "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.secretKey)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", "", "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", string(body), fmt.Errorf("stripe checkout failed: %s", strings.TrimSpace(string(body)))
	}
	var out struct {
		ID  string `json:"id"`
		URL string `json:"url"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", "", string(body), err
	}
	if out.ID == "" || out.URL == "" {
		return "", "", string(body), errors.New("stripe checkout response missing id or url")
	}
	return out.ID, out.URL, string(body), nil
}

func (c *StripeClient) VerifyWebhook(payload []byte, signature string) error {
	if c.webhookSecret == "" {
		return errors.New("stripe webhook secret is not configured")
	}
	var timestamp, expected string
	for _, part := range strings.Split(signature, ",") {
		pair := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(pair) != 2 {
			continue
		}
		switch pair[0] {
		case "t":
			timestamp = pair[1]
		case "v1":
			expected = pair[1]
		}
	}
	if timestamp == "" || expected == "" {
		return errors.New("invalid stripe signature header")
	}
	signed := timestamp + "." + string(payload)
	mac := hmac.New(sha256.New, []byte(c.webhookSecret))
	mac.Write([]byte(signed))
	actual := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(actual), []byte(expected)) {
		return errors.New("invalid stripe webhook signature")
	}
	return nil
}

type StripeWebhookEvent struct {
	Type string `json:"type"`
	Data struct {
		Object struct {
			ID            string            `json:"id"`
			PaymentStatus string            `json:"payment_status"`
			AmountTotal   int               `json:"amount_total"`
			Currency      string            `json:"currency"`
			Metadata      map[string]string `json:"metadata"`
		} `json:"object"`
	} `json:"data"`
}

type PayPalClient struct {
	clientID     string
	clientSecret string
	baseURL      string
	httpClient   *http.Client
}

func NewPayPalClient(clientID, clientSecret, environment string) *PayPalClient {
	baseURL := "https://api-m.sandbox.paypal.com"
	if strings.EqualFold(strings.TrimSpace(environment), "live") {
		baseURL = "https://api-m.paypal.com"
	}
	return &PayPalClient{
		clientID:     strings.TrimSpace(clientID),
		clientSecret: strings.TrimSpace(clientSecret),
		baseURL:      baseURL,
		httpClient:   &http.Client{Timeout: 20 * time.Second},
	}
}

func (c *PayPalClient) Enabled() bool {
	return c.clientID != "" && c.clientSecret != ""
}

func (c *PayPalClient) CreateOrder(transactionID int64, amountCents int, currency, successURL, cancelURL string) (string, string, string, error) {
	if !c.Enabled() {
		return "", "", "", errors.New("paypal is not configured")
	}
	token, err := c.accessToken()
	if err != nil {
		return "", "", "", err
	}
	payload := map[string]any{
		"intent": "CAPTURE",
		"purchase_units": []map[string]any{
			{
				"reference_id": fmt.Sprint(transactionID),
				"amount": map[string]string{
					"currency_code": strings.ToUpper(currency),
					"value":         centsToDecimal(amountCents),
				},
			},
		},
		"application_context": map[string]string{
			"return_url": successURL + "&provider=paypal&transaction_id=" + fmt.Sprint(transactionID),
			"cancel_url": cancelURL,
		},
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/v2/checkout/orders", bytes.NewReader(body))
	if err != nil {
		return "", "", "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", "", "", err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", string(respBody), fmt.Errorf("paypal order create failed: %s", strings.TrimSpace(string(respBody)))
	}
	var out struct {
		ID    string `json:"id"`
		Links []struct {
			Href string `json:"href"`
			Rel  string `json:"rel"`
		} `json:"links"`
	}
	if err := json.Unmarshal(respBody, &out); err != nil {
		return "", "", string(respBody), err
	}
	approveURL := ""
	for _, link := range out.Links {
		if link.Rel == "approve" {
			approveURL = link.Href
			break
		}
	}
	if out.ID == "" || approveURL == "" {
		return "", "", string(respBody), errors.New("paypal order response missing id or approve link")
	}
	return out.ID, approveURL, string(respBody), nil
}

func (c *PayPalClient) CaptureOrder(orderID string) (string, error) {
	if !c.Enabled() {
		return "", errors.New("paypal is not configured")
	}
	token, err := c.accessToken()
	if err != nil {
		return "", err
	}
	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/v2/checkout/orders/"+url.PathEscape(orderID)+"/capture", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return string(body), fmt.Errorf("paypal order capture failed: %s", strings.TrimSpace(string(body)))
	}
	return string(body), nil
}

func (c *PayPalClient) accessToken() (string, error) {
	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/v1/oauth2/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.SetBasicAuth(c.clientID, c.clientSecret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("paypal token request failed: %s", strings.TrimSpace(string(body)))
	}
	var out struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", err
	}
	if out.AccessToken == "" {
		return "", errors.New("paypal token response missing access token")
	}
	return out.AccessToken, nil
}

func centsToDecimal(cents int) string {
	return strconv.FormatFloat(float64(cents)/100.0, 'f', 2, 64)
}
