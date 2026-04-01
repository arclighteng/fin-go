package simplefin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/arclighteng/fin-go/internal/models"
	"github.com/arclighteng/fin-go/internal/money"
)

// Client talks to the SimpleFIN API.
type Client struct {
	accessURL string
	http      *http.Client
}

// NewClient creates a SimpleFIN client from the access URL.
func NewClient(accessURL string) (*Client, error) {
	u, err := url.Parse(accessURL)
	if err != nil {
		return nil, fmt.Errorf("invalid access URL: %w", err)
	}
	if u.Scheme != "https" {
		return nil, fmt.Errorf("access URL must use HTTPS, got %s", u.Scheme)
	}

	return &Client{
		accessURL: accessURL,
		http: &http.Client{
			Timeout: 30 * time.Second,
		},
	}, nil
}

// redactedURL returns the URL with credentials masked for logging.
func (c *Client) redactedURL() string {
	u, err := url.Parse(c.accessURL)
	if err != nil {
		return "[invalid URL]"
	}
	return u.Redacted()
}

// accountsResponse is the SimpleFIN /accounts response.
type accountsResponse struct {
	Errors   []string          `json:"errors"`
	Accounts []simpleFinAcct   `json:"accounts"`
}

type simpleFinAcct struct {
	ID           string             `json:"id"`
	Org          simpleFinOrg       `json:"org"`
	Name         string             `json:"name"`
	Currency     string             `json:"currency"`
	Balance      json.Number        `json:"balance"`
	Transactions []simpleFinTxn     `json:"transactions"`
}

type simpleFinOrg struct {
	Name string `json:"name"`
}

type simpleFinTxn struct {
	ID          string      `json:"id"`
	Posted      int64       `json:"posted"`
	Amount      json.Number `json:"amount"`
	Description string      `json:"description"`
	Payee       string      `json:"payee"`
	Pending     bool        `json:"pending"`
}

// FetchResult holds the results of a SimpleFIN sync.
type FetchResult struct {
	Accounts     []models.Account
	Transactions []models.Transaction
}

// Fetch retrieves accounts and transactions from SimpleFIN.
func (c *Client) Fetch(ctx context.Context, lookbackDays int) (*FetchResult, error) {
	endpoint := strings.TrimSuffix(c.accessURL, "/") + "/accounts"

	if lookbackDays > 0 {
		since := time.Now().AddDate(0, 0, -lookbackDays).Unix()
		endpoint = fmt.Sprintf("%s?start-date=%d", endpoint, since)
	}

	log.Printf("SimpleFIN: fetching from %s (lookback: %d days)", c.redactedURL(), lookbackDays)

	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch accounts: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("SimpleFIN returned %d: %s", resp.StatusCode, string(body))
	}

	var apiResp accountsResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if len(apiResp.Errors) > 0 {
		return nil, fmt.Errorf("SimpleFIN errors: %v", apiResp.Errors)
	}

	result := &FetchResult{}

	for _, acct := range apiResp.Accounts {
		result.Accounts = append(result.Accounts, models.Account{
			AccountID:   acct.ID,
			Institution: acct.Org.Name,
			Name:        acct.Name,
			Currency:    acct.Currency,
		})

		for _, txn := range acct.Transactions {
			amountStr := txn.Amount.String()
			cents, err := money.ParseToCentsAllowLarge(amountStr)
			if err != nil {
				log.Printf("SimpleFIN: skip txn %s, bad amount %q: %v", txn.ID, amountStr, err)
				continue
			}

			fp := fmt.Sprintf("%s|%s|%d|%s", acct.ID, txn.ID, txn.Posted, amountStr)

			merchant := txn.Payee
			description := txn.Description

			result.Transactions = append(result.Transactions, models.Transaction{
				AccountID:   acct.ID,
				PostedAt:    time.Unix(txn.Posted, 0).UTC().Truncate(24 * time.Hour),
				AmountCents: cents,
				Currency:    acct.Currency,
				Description: description,
				Merchant:    merchant,
				SourceTxnID: txn.ID,
				Fingerprint: fp,
				Pending:     txn.Pending,
			})
		}
	}

	log.Printf("SimpleFIN: fetched %d accounts, %d transactions", len(result.Accounts), len(result.Transactions))
	return result, nil
}
