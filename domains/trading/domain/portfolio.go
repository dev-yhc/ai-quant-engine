// Package domain contains transport-neutral trading portfolio values.
package domain

import "time"

// Portfolio is a point-in-time stock-and-cash trading book. Monetary amounts
// and ratios remain decimal strings so callers do not lose precision in JSON.
// Weights are fractions of TotalMarketValue (for example, 0.125 is 12.5%).
type Portfolio struct {
	AsOf                time.Time `json:"as_of"`
	ReportingCurrency   string    `json:"reporting_currency"`
	USDKRWRate          string    `json:"usd_krw_rate,omitempty"`
	TotalMarketValue    string    `json:"total_market_value"`
	ProfitLossRate      string    `json:"profit_loss_rate"`
	DailyProfitLossRate string    `json:"daily_profit_loss_rate"`
	Holdings            []Holding `json:"holdings"`
	Cash                []Cash    `json:"cash"`
}

// Holding describes one held Korean or US equity. Amount fields before the
// KRW conversion use Currency; MarketValueKRW and Weight use the portfolio's
// reporting currency.
type Holding struct {
	Instrument           string `json:"instrument"`
	Symbol               string `json:"symbol"`
	Name                 string `json:"name"`
	MarketCountry        string `json:"market_country"`
	Currency             string `json:"currency"`
	Quantity             string `json:"quantity"`
	LastPrice            string `json:"last_price"`
	AveragePurchasePrice string `json:"average_purchase_price"`
	MarketValue          string `json:"market_value"`
	MarketValueAfterCost string `json:"market_value_after_cost"`
	ProfitLoss           string `json:"profit_loss"`
	ProfitLossRate       string `json:"profit_loss_rate"`
	DailyProfitLoss      string `json:"daily_profit_loss"`
	DailyProfitLossRate  string `json:"daily_profit_loss_rate"`
	MarketValueKRW       string `json:"market_value_krw"`
	Weight               string `json:"weight"`
}

// Cash is the account's immediately available buying power. Its Weight uses
// the same TotalMarketValue denominator as stock holdings.
type Cash struct {
	Currency    string `json:"currency"`
	BuyingPower string `json:"buying_power"`
	ValueKRW    string `json:"value_krw"`
	Weight      string `json:"weight"`
}
