// Package toss adapts a validated order intent to Toss Securities Open API.
package toss

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yhc/quant-engine-go/apps/trading-engine/internal/domain"
)

const defaultBaseURL = "https://openapi.tossinvest.com"

type Config struct {
	ClientID, ClientSecret string
	AccountSeq             int64
	BaseURL                string
	HTTPClient             *http.Client
}
type Client struct {
	config         Config
	http           *http.Client
	mu             sync.Mutex
	token          string
	tokenExpiresAt time.Time
}

func New(config Config) (*Client, error) {
	if config.ClientID == "" || config.ClientSecret == "" || config.AccountSeq <= 0 {
		return nil, fmt.Errorf("TOSS_CLIENT_ID, TOSS_CLIENT_SECRET, and TOSS_ACCOUNT_SEQ are required")
	}
	if config.BaseURL == "" {
		config.BaseURL = defaultBaseURL
	}
	if config.HTTPClient == nil {
		config.HTTPClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &Client{config: config, http: config.HTTPClient}, nil
}

type apiError struct {
	Status  int
	Code    string
	Message string
}

func (e apiError) Error() string {
	return fmt.Sprintf("toss API status=%d code=%s: %s", e.Status, e.Code, e.Message)
}
func (e apiError) Retryable() bool { return e.Status == 409 || e.Status == 429 || e.Status >= 500 }

func (c *Client) Submit(ctx context.Context, intent domain.Intent) (string, error) {
	token, err := c.accessToken(ctx)
	if err != nil {
		return "", err
	}
	_, symbol, _ := intent.MarketAndSymbol()
	payload := map[string]any{"clientOrderId": intent.TossClientOrderID(), "symbol": symbol, "side": intent.Side, "orderType": intent.OrderType}
	if intent.Quantity != "" {
		payload["quantity"] = intent.Quantity
	} else {
		payload["orderAmount"] = intent.OrderAmount
	}
	if intent.LimitPrice != "" {
		payload["price"] = intent.LimitPrice
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode Toss order: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.config.BaseURL+"/api/v1/orders", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Tossinvest-Account", strconv.FormatInt(c.config.AccountSeq, 10))
	req.Header.Set("Content-Type", "application/json")
	response, err := c.http.Do(req)
	if err != nil {
		return "", temporary{fmt.Errorf("call Toss order API: %w", err)}
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return "", temporary{fmt.Errorf("read Toss order API response: %w", err)}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", parseError(response.StatusCode, data)
	}
	var decoded struct {
		Result struct {
			OrderID string `json:"orderId"`
		} `json:"result"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return "", fmt.Errorf("decode Toss order response: %w", err)
	}
	if decoded.Result.OrderID == "" {
		return "", fmt.Errorf("Toss order response did not include orderId")
	}
	return decoded.Result.OrderID, nil
}

type temporary struct{ error }

func (temporary) Retryable() bool { return true }
func (c *Client) accessToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && time.Until(c.tokenExpiresAt) > time.Minute {
		return c.token, nil
	}
	form := url.Values{"grant_type": {"client_credentials"}, "client_id": {c.config.ClientID}, "client_secret": {c.config.ClientSecret}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.config.BaseURL+"/oauth2/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := c.http.Do(req)
	if err != nil {
		return "", temporary{fmt.Errorf("request Toss access token: %w", err)}
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return "", temporary{err}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", parseError(response.StatusCode, data)
	}
	var token struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(data, &token); err != nil {
		return "", fmt.Errorf("decode Toss token response: %w", err)
	}
	if token.AccessToken == "" || token.ExpiresIn <= 0 {
		return "", fmt.Errorf("invalid Toss token response")
	}
	c.token = token.AccessToken
	c.tokenExpiresAt = time.Now().Add(time.Duration(token.ExpiresIn) * time.Second)
	return c.token, nil
}
func parseError(status int, data []byte) error {
	var decoded struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.Unmarshal(data, &decoded)
	return apiError{Status: status, Code: decoded.Error.Code, Message: decoded.Error.Message}
}
