// Package application coordinates market-data use cases.
package application

import "github.com/yhc/quant-engine-go/domains/marketdata/domain"

// LatestQuote is a placeholder use case boundary. Add a concrete provider adapter
// when an external feed is selected; do not introduce an interface before then.
func LatestQuote(symbol string) (domain.Quote, bool) {
	return domain.Quote{}, false
}
