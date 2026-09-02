// Package toss adapts a validated order intent to Toss Securities Open API.
package toss

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yhc/quant-engine-go/apps/trading-engine/internal/domain"
	tradingdomain "github.com/yhc/quant-engine-go/domains/trading/domain"
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
	c.authorizeAccount(req, token)
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

// Portfolio retrieves every stock holding plus available cash buying power and
// turns mixed KRW/USD values into a single KRW trading book. Toss does not
// expose options or bonds through the holdings endpoint.
func (c *Client) Portfolio(ctx context.Context) (tradingdomain.Portfolio, error) {
	token, err := c.accessToken(ctx)
	if err != nil {
		return tradingdomain.Portfolio{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.config.BaseURL+"/api/v1/holdings", nil)
	if err != nil {
		return tradingdomain.Portfolio{}, err
	}
	c.authorizeAccount(req, token)
	response, err := c.http.Do(req)
	if err != nil {
		return tradingdomain.Portfolio{}, temporary{fmt.Errorf("call Toss holdings API: %w", err)}
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return tradingdomain.Portfolio{}, temporary{fmt.Errorf("read Toss holdings response: %w", err)}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return tradingdomain.Portfolio{}, parseError(response.StatusCode, data)
	}
	var decoded holdingsResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		return tradingdomain.Portfolio{}, fmt.Errorf("decode Toss holdings response: %w", err)
	}
	return c.toPortfolio(ctx, token, decoded.Result)
}

// USDToKRWRate exposes the current conversion needed to turn a KRW-denominated
// strategy target into Toss's US market-order amount.
func (c *Client) USDToKRWRate(ctx context.Context) (string, error) {
	token, err := c.accessToken(ctx)
	if err != nil {
		return "", err
	}
	return c.usdKRWRate(ctx, token)
}

func (c *Client) toPortfolio(ctx context.Context, token string, overview holdingsOverview) (tradingdomain.Portfolio, error) {
	krwTotal, err := decimal(overview.MarketValue.Amount.KRW)
	if err != nil {
		return tradingdomain.Portfolio{}, fmt.Errorf("invalid KRW holdings total: %w", err)
	}
	usdTotal, err := nullableDecimal(overview.MarketValue.Amount.USD)
	if err != nil {
		return tradingdomain.Portfolio{}, fmt.Errorf("invalid USD holdings total: %w", err)
	}
	krwCash, err := c.cashBuyingPower(ctx, token, "KRW")
	if err != nil {
		return tradingdomain.Portfolio{}, err
	}
	usdCash, err := c.cashBuyingPower(ctx, token, "USD")
	if err != nil {
		return tradingdomain.Portfolio{}, err
	}
	book := tradingdomain.Portfolio{
		AsOf:              time.Now().UTC(),
		ReportingCurrency: "KRW",
		Holdings:          make([]tradingdomain.Holding, 0, len(overview.Items)),
		Cash:              make([]tradingdomain.Cash, 0, 2),
	}
	usdKRWRate := new(big.Rat)
	if usdTotal.Sign() != 0 || usdCash.Sign() != 0 {
		rate, err := c.usdKRWRate(ctx, token)
		if err != nil {
			return tradingdomain.Portfolio{}, err
		}
		usdKRWRate, err = decimal(rate)
		if err != nil {
			return tradingdomain.Portfolio{}, fmt.Errorf("invalid USD/KRW exchange rate: %w", err)
		}
		book.USDKRWRate = rate
	}
	totalKRW := new(big.Rat).Set(krwTotal)
	totalKRW.Add(totalKRW, new(big.Rat).Mul(usdTotal, usdKRWRate))
	totalKRW.Add(totalKRW, krwCash)
	totalKRW.Add(totalKRW, new(big.Rat).Mul(usdCash, usdKRWRate))
	book.TotalMarketValue = decimalString(totalKRW)
	profitLoss, err := priceInKRW(overview.ProfitLoss.Amount, usdKRWRate)
	if err != nil {
		return tradingdomain.Portfolio{}, fmt.Errorf("invalid profit/loss total: %w", err)
	}
	dailyProfitLoss, err := priceInKRW(overview.DailyProfitLoss.Amount, usdKRWRate)
	if err != nil {
		return tradingdomain.Portfolio{}, fmt.Errorf("invalid daily profit/loss total: %w", err)
	}
	if totalKRW.Sign() != 0 {
		book.ProfitLossRate = decimalString(new(big.Rat).Quo(profitLoss, totalKRW))
		book.DailyProfitLossRate = decimalString(new(big.Rat).Quo(dailyProfitLoss, totalKRW))
	}
	for _, item := range overview.Items {
		marketValue, err := decimal(item.MarketValue.Amount)
		if err != nil {
			return tradingdomain.Portfolio{}, fmt.Errorf("invalid market value for %s: %w", item.Symbol, err)
		}
		marketValueKRW := new(big.Rat).Set(marketValue)
		switch item.Currency {
		case "KRW":
		case "USD":
			marketValueKRW.Mul(marketValueKRW, usdKRWRate)
		default:
			return tradingdomain.Portfolio{}, fmt.Errorf("unsupported holding currency %q for %s", item.Currency, item.Symbol)
		}
		weight := new(big.Rat)
		if totalKRW.Sign() != 0 {
			weight.Quo(marketValueKRW, totalKRW)
		}
		book.Holdings = append(book.Holdings, tradingdomain.Holding{
			Instrument:           item.MarketCountry + ":" + item.Symbol,
			Symbol:               item.Symbol,
			Name:                 item.Name,
			MarketCountry:        item.MarketCountry,
			Currency:             item.Currency,
			Quantity:             item.Quantity,
			LastPrice:            item.LastPrice,
			AveragePurchasePrice: item.AveragePurchasePrice,
			MarketValue:          item.MarketValue.Amount,
			MarketValueAfterCost: item.MarketValue.AmountAfterCost,
			ProfitLoss:           item.ProfitLoss.Amount,
			ProfitLossRate:       item.ProfitLoss.Rate,
			DailyProfitLoss:      item.DailyProfitLoss.Amount,
			DailyProfitLossRate:  item.DailyProfitLoss.Rate,
			MarketValueKRW:       decimalString(marketValueKRW),
			Weight:               decimalString(weight),
		})
	}
	book.Cash = append(book.Cash, cashPosition("KRW", krwCash, new(big.Rat), totalKRW))
	book.Cash = append(book.Cash, cashPosition("USD", usdCash, usdKRWRate, totalKRW))
	return book, nil
}

func (c *Client) cashBuyingPower(ctx context.Context, token, currency string) (*big.Rat, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.config.BaseURL+"/api/v1/buying-power?currency="+currency, nil)
	if err != nil {
		return nil, err
	}
	c.authorizeAccount(req, token)
	response, err := c.http.Do(req)
	if err != nil {
		return nil, temporary{fmt.Errorf("call Toss %s buying-power API: %w", currency, err)}
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return nil, temporary{fmt.Errorf("read Toss %s buying-power response: %w", currency, err)}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, parseError(response.StatusCode, data)
	}
	var decoded struct {
		Result struct {
			CashBuyingPower string `json:"cashBuyingPower"`
		} `json:"result"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return nil, fmt.Errorf("decode Toss %s buying-power response: %w", currency, err)
	}
	value, err := decimal(decoded.Result.CashBuyingPower)
	if err != nil {
		return nil, fmt.Errorf("invalid Toss %s buying power: %w", currency, err)
	}
	return value, nil
}

func (c *Client) usdKRWRate(ctx context.Context, token string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.config.BaseURL+"/api/v1/exchange-rate?baseCurrency=USD&quoteCurrency=KRW", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	response, err := c.http.Do(req)
	if err != nil {
		return "", temporary{fmt.Errorf("call Toss exchange-rate API: %w", err)}
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return "", temporary{fmt.Errorf("read Toss exchange-rate response: %w", err)}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", parseError(response.StatusCode, data)
	}
	var decoded struct {
		Result struct {
			Rate string `json:"rate"`
		} `json:"result"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return "", fmt.Errorf("decode Toss exchange-rate response: %w", err)
	}
	if decoded.Result.Rate == "" {
		return "", fmt.Errorf("Toss exchange-rate response did not include rate")
	}
	return decoded.Result.Rate, nil
}

func (c *Client) authorizeAccount(req *http.Request, token string) {
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Tossinvest-Account", strconv.FormatInt(c.config.AccountSeq, 10))
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

type holdingsResponse struct {
	Result holdingsOverview `json:"result"`
}
type holdingsOverview struct {
	MarketValue struct {
		Amount price `json:"amount"`
	} `json:"marketValue"`
	ProfitLoss struct {
		Amount price `json:"amount"`
	} `json:"profitLoss"`
	DailyProfitLoss struct {
		Amount price `json:"amount"`
	} `json:"dailyProfitLoss"`
	Items []holding `json:"items"`
}
type price struct {
	KRW string  `json:"krw"`
	USD *string `json:"usd"`
}
type holding struct {
	Symbol               string `json:"symbol"`
	Name                 string `json:"name"`
	MarketCountry        string `json:"marketCountry"`
	Currency             string `json:"currency"`
	Quantity             string `json:"quantity"`
	LastPrice            string `json:"lastPrice"`
	AveragePurchasePrice string `json:"averagePurchasePrice"`
	MarketValue          struct {
		Amount          string `json:"amount"`
		AmountAfterCost string `json:"amountAfterCost"`
	} `json:"marketValue"`
	ProfitLoss struct {
		Amount string `json:"amount"`
		Rate   string `json:"rate"`
	} `json:"profitLoss"`
	DailyProfitLoss struct {
		Amount string `json:"amount"`
		Rate   string `json:"rate"`
	} `json:"dailyProfitLoss"`
}

func decimal(value string) (*big.Rat, error) {
	if value == "" {
		return nil, fmt.Errorf("empty decimal")
	}
	result, ok := new(big.Rat).SetString(value)
	if !ok {
		return nil, fmt.Errorf("%q is not a decimal", value)
	}
	return result, nil
}
func nullableDecimal(value *string) (*big.Rat, error) {
	if value == nil {
		return new(big.Rat), nil
	}
	return decimal(*value)
}
func priceInKRW(value price, usdKRWRate *big.Rat) (*big.Rat, error) {
	krw, err := decimal(value.KRW)
	if err != nil {
		return nil, err
	}
	usd, err := nullableDecimal(value.USD)
	if err != nil {
		return nil, err
	}
	return new(big.Rat).Add(krw, new(big.Rat).Mul(usd, usdKRWRate)), nil
}
func decimalString(value *big.Rat) string {
	text := value.FloatString(8)
	text = strings.TrimRight(strings.TrimRight(text, "0"), ".")
	if text == "" || text == "-0" {
		return "0"
	}
	return text
}

func cashPosition(currency string, amount, exchangeRate, totalKRW *big.Rat) tradingdomain.Cash {
	valueKRW := new(big.Rat).Set(amount)
	if currency == "USD" {
		valueKRW.Mul(valueKRW, exchangeRate)
	}
	weight := new(big.Rat)
	if totalKRW.Sign() != 0 {
		weight.Quo(valueKRW, totalKRW)
	}
	return tradingdomain.Cash{Currency: currency, BuyingPower: decimalString(amount), ValueKRW: decimalString(valueKRW), Weight: decimalString(weight)}
}
