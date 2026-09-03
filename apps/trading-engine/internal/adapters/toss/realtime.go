package toss

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/yhc/quant-engine-go/apps/trading-engine/internal/domain"
	"golang.org/x/net/websocket"
)

const websocketURL = "wss://openapi-ws.tossinvest.com/ws/v1"

// QuoteLevel is one price and available volume in an order book.
type QuoteLevel struct {
	Price  string `json:"price"`
	Volume string `json:"volume"`
}

// Trade is a public market trade. It is distinct from a personal order fill.
type Trade struct {
	Price     string    `json:"price"`
	Volume    string    `json:"volume"`
	Timestamp time.Time `json:"timestamp"`
	Currency  string    `json:"currency"`
}

// MarketSnapshot is the latest known state for one instrument. The initial
// state is fetched through REST; subsequent values come from the WebSocket.
type MarketSnapshot struct {
	Instrument string       `json:"instrument"`
	Timestamp  time.Time    `json:"timestamp"`
	ReceivedAt time.Time    `json:"receivedAt"`
	Currency   string       `json:"currency"`
	Asks       []QuoteLevel `json:"asks"`
	Bids       []QuoteLevel `json:"bids"`
	Trades     []Trade      `json:"trades"`
}

// BestBid returns the highest valid bid, regardless of ordering in a source
// payload. It is the passive LIMIT price for a buy order.
func (s MarketSnapshot) BestBid() (QuoteLevel, bool) { return bestLevel(s.Bids, true) }

// BestAsk returns the lowest valid ask, regardless of ordering in a source
// payload. It is the passive LIMIT price for a sell order.
func (s MarketSnapshot) BestAsk() (QuoteLevel, bool) { return bestLevel(s.Asks, false) }

// PassiveLimitPrice selects the inside quote without crossing the spread:
// BUY uses the highest bid and SELL uses the lowest ask.
func (s MarketSnapshot) PassiveLimitPrice(side domain.Side) (QuoteLevel, bool) {
	switch side {
	case domain.Buy:
		return s.BestBid()
	case domain.Sell:
		return s.BestAsk()
	default:
		return QuoteLevel{}, false
	}
}

func bestLevel(levels []QuoteLevel, highest bool) (QuoteLevel, bool) {
	var best QuoteLevel
	var bestPrice *big.Rat
	for _, level := range levels {
		price, ok := new(big.Rat).SetString(level.Price)
		if !ok || price.Sign() <= 0 {
			continue
		}
		if bestPrice == nil || (highest && price.Cmp(bestPrice) > 0) || (!highest && price.Cmp(bestPrice) < 0) {
			best, bestPrice = level, price
		}
	}
	return best, bestPrice != nil
}

// OrderEvent contains the latest personal-order state supplied by Toss.
// It is stored by broker order ID and allows callers to verify partial and
// complete fills without mistaking market-wide trade ticks for own fills.
type OrderEvent struct {
	Event      string     `json:"event"`
	AccountSeq string     `json:"accountSeq"`
	ReceivedAt time.Time  `json:"receivedAt"`
	Order      OrderState `json:"order"`
}

type OrderState struct {
	OrderID     string         `json:"orderId"`
	Symbol      string         `json:"symbol"`
	Side        string         `json:"side"`
	OrderType   string         `json:"orderType"`
	TimeInForce string         `json:"timeInForce"`
	Status      string         `json:"status"`
	Price       *string        `json:"price"`
	Quantity    string         `json:"quantity"`
	OrderAmount *string        `json:"orderAmount"`
	Currency    string         `json:"currency"`
	OrderedAt   time.Time      `json:"orderedAt"`
	CanceledAt  *time.Time     `json:"canceledAt"`
	Execution   OrderExecution `json:"execution"`
}

type OrderExecution struct {
	FilledQuantity     string  `json:"filledQuantity"`
	AverageFilledPrice *string `json:"averageFilledPrice"`
	FilledAmount       *string `json:"filledAmount"`
	Commission         *string `json:"commission"`
	Tax                *string `json:"tax"`
	SettlementDate     *string `json:"settlementDate"`
}

// Realtime owns an in-memory, continuously refreshed market and order view.
// WebSocket delivery is intentionally separated from persistent order storage:
// a reconnect always refreshes the REST snapshots before accepting new events.
type Realtime struct {
	client *Client

	mu        sync.RWMutex
	market    map[string]MarketSnapshot
	orders    map[string]OrderEvent
	lastError string
}

func NewRealtime(client *Client) *Realtime {
	return &Realtime{client: client, market: make(map[string]MarketSnapshot), orders: make(map[string]OrderEvent)}
}

// Run opens the feed until ctx is cancelled. It reconnects with capped
// exponential backoff and restores REST snapshots after every reconnect.
func (r *Realtime) Run(ctx context.Context, instruments []string) error {
	valid, err := normalizeInstruments(instruments)
	if err != nil {
		return err
	}
	// Each instrument uses two topics (trade and orderbook); personal:order
	// consumes one more of Toss's 100-topic per-connection limit.
	if len(valid)*2+1 > 100 {
		return fmt.Errorf("%d instruments exceed Toss WebSocket's 100-topic connection limit", len(valid))
	}
	delay := time.Second
	for {
		if err := r.seed(ctx, valid); err != nil && ctx.Err() == nil {
			r.setError(fmt.Sprintf("seed market snapshots: %v", err))
			if !wait(ctx, delay) {
				return nil
			}
			delay = nextDelay(delay)
			continue
		}
		err = r.connect(ctx, valid)
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			r.setError(err.Error())
		}
		if !wait(ctx, delay) {
			return nil
		}
		delay = nextDelay(delay)
	}
}

// Snapshot returns a copy of the latest snapshot. The instrument must be in
// engine format, for example KR:005930 or US:AAPL.
func (r *Realtime) Snapshot(instrument string) (MarketSnapshot, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	snapshot, ok := r.market[instrument]
	return cloneSnapshot(snapshot), ok
}

func (r *Realtime) Order(orderID string) (OrderEvent, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	event, ok := r.orders[orderID]
	return event, ok
}

func (r *Realtime) LastError() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.lastError
}

func (r *Realtime) seed(ctx context.Context, instruments []string) error {
	for _, instrument := range instruments {
		snapshot, err := r.client.MarketSnapshot(ctx, instrument)
		if err != nil {
			return fmt.Errorf("%s: %w", instrument, err)
		}
		r.mu.Lock()
		r.market[instrument] = snapshot
		r.mu.Unlock()
	}
	orders, err := r.client.CurrentDayOrders(ctx)
	if err != nil {
		return fmt.Errorf("synchronize personal orders: %w", err)
	}
	now := time.Now().UTC()
	r.mu.Lock()
	for _, order := range orders {
		r.orders[order.OrderID] = OrderEvent{Event: "SYNC", AccountSeq: fmt.Sprint(r.client.config.AccountSeq), ReceivedAt: now, Order: order}
	}
	r.mu.Unlock()
	return nil
}

func (r *Realtime) connect(ctx context.Context, instruments []string) error {
	token, err := r.client.accessToken(ctx)
	if err != nil {
		return err
	}
	location, _ := url.Parse(websocketURL)
	origin, _ := url.Parse("https://openapi.tossinvest.com")
	connection, err := websocket.DialConfig(&websocket.Config{
		Location: location,
		Origin:   origin,
		Header:   http.Header{"Authorization": []string{"Bearer " + token}},
		Dialer:   &net.Dialer{Timeout: 15 * time.Second},
	})
	if err != nil {
		return fmt.Errorf("dial Toss WebSocket: %w", err)
	}
	r.clearError()
	defer connection.Close()
	closed := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = connection.Close()
		case <-closed:
		}
	}()
	defer close(closed)

	declaration := subscriptionDeclaration(instruments, r.client.config.AccountSeq)
	if err := websocket.JSON.Send(connection, declaration); err != nil {
		return fmt.Errorf("declare Toss WebSocket subscriptions: %w", err)
	}
	return r.read(ctx, connection)
}

func (r *Realtime) read(ctx context.Context, connection *websocket.Conn) error {
	ping := time.NewTicker(time.Minute)
	defer ping.Stop()
	frames := make(chan json.RawMessage, 1)
	errs := make(chan error, 1)
	done := make(chan struct{})
	defer close(done)
	go func() {
		for {
			var raw json.RawMessage
			if err := websocket.JSON.Receive(connection, &raw); err != nil {
				select {
				case errs <- err:
				case <-done:
				}
				return
			}
			select {
			case frames <- raw:
			case <-done:
				return
			}
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-errs:
			return fmt.Errorf("read Toss WebSocket: %w", err)
		case <-ping.C:
			if err := websocket.Message.Send(connection, "PING"); err != nil {
				return fmt.Errorf("send Toss WebSocket keepalive: %w", err)
			}
		case frame := <-frames:
			if err := r.applyFrame(frame); err != nil {
				return err
			}
		}
	}
}

func (r *Realtime) applyFrame(raw json.RawMessage) error {
	var frame struct {
		Type  string          `json:"type"`
		Topic string          `json:"topic"`
		Data  json.RawMessage `json:"data"`
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &frame); err != nil {
		return fmt.Errorf("decode Toss WebSocket frame: %w", err)
	}
	if frame.Type == "error" {
		if frame.Error == nil {
			return fmt.Errorf("Toss WebSocket error frame without error")
		}
		return fmt.Errorf("Toss WebSocket error %s: %s", frame.Error.Code, frame.Error.Message)
	}
	if frame.Type != "message" {
		return nil // subscriptions ack and pong do not change state.
	}
	now := time.Now().UTC()
	switch {
	case strings.HasPrefix(frame.Topic, "orderbook:"):
		instrument, err := topicInstrument(frame.Topic, "orderbook")
		if err != nil {
			return err
		}
		var book orderbookPayload
		if err := json.Unmarshal(frame.Data, &book); err != nil {
			return fmt.Errorf("decode orderbook event: %w", err)
		}
		r.mu.Lock()
		current := r.market[instrument]
		current.Instrument, current.Timestamp, current.ReceivedAt, current.Currency = instrument, book.Timestamp.Time, now, book.Currency
		current.Asks, current.Bids = book.Asks, book.Bids
		r.market[instrument] = current
		r.mu.Unlock()
	case strings.HasPrefix(frame.Topic, "trade:"):
		instrument, err := topicInstrument(frame.Topic, "trade")
		if err != nil {
			return err
		}
		var trade tradePayload
		if err := json.Unmarshal(frame.Data, &trade); err != nil {
			return fmt.Errorf("decode trade event: %w", err)
		}
		r.mu.Lock()
		current := r.market[instrument]
		current.Instrument, current.ReceivedAt = instrument, now
		if current.Currency == "" {
			current.Currency = trade.Currency
		}
		current.Trades = append([]Trade{{Price: trade.Price, Volume: trade.Volume, Timestamp: trade.Timestamp.Time, Currency: trade.Currency}}, current.Trades...)
		if len(current.Trades) > 50 {
			current.Trades = current.Trades[:50]
		}
		r.market[instrument] = current
		r.mu.Unlock()
	case strings.HasPrefix(frame.Topic, "personal:order:"):
		var event OrderEvent
		if err := json.Unmarshal(frame.Data, &event); err != nil {
			return fmt.Errorf("decode personal order event: %w", err)
		}
		event.ReceivedAt = now
		if event.Order.OrderID == "" {
			return fmt.Errorf("personal order event without orderId")
		}
		r.mu.Lock()
		r.orders[event.Order.OrderID] = event
		r.mu.Unlock()
	}
	return nil
}

func subscriptionDeclaration(instruments []string, accountSeq int64) []map[string]any {
	grouped := map[string][]string{"trade:kr": nil, "trade:us": nil, "orderbook:kr": nil, "orderbook:us": nil}
	for _, instrument := range instruments {
		market, symbol, _ := instrumentParts(instrument)
		key := strings.ToLower(market)
		grouped["trade:"+key] = append(grouped["trade:"+key], symbol)
		grouped["orderbook:"+key] = append(grouped["orderbook:"+key], symbol)
	}
	declaration := make([]map[string]any, 0, 5)
	for _, kind := range []string{"trade:kr", "trade:us", "orderbook:kr", "orderbook:us"} {
		if len(grouped[kind]) > 0 {
			declaration = append(declaration, map[string]any{"type": kind, "codes": grouped[kind]})
		}
	}
	return append(declaration, map[string]any{"type": "personal:order", "codes": []string{fmt.Sprint(accountSeq)}})
}

func normalizeInstruments(instruments []string) ([]string, error) {
	unique := make(map[string]struct{}, len(instruments))
	for _, instrument := range instruments {
		market, symbol, err := instrumentParts(instrument)
		if err != nil {
			return nil, err
		}
		unique[market+":"+symbol] = struct{}{}
	}
	result := make([]string, 0, len(unique))
	for instrument := range unique {
		result = append(result, instrument)
	}
	sort.Strings(result)
	return result, nil
}

func instrumentParts(instrument string) (string, string, error) {
	parts := strings.SplitN(instrument, ":", 2)
	if len(parts) != 2 || (parts[0] != "KR" && parts[0] != "US") || parts[1] == "" {
		return "", "", fmt.Errorf("instrument must be US:<ticker> or KR:<six-digit-symbol>")
	}
	return parts[0], parts[1], nil
}

func topicInstrument(topic, expected string) (string, error) {
	parts := strings.Split(topic, ":")
	if len(parts) != 3 || parts[0] != expected {
		return "", fmt.Errorf("invalid %s topic %q", expected, topic)
	}
	market := strings.ToUpper(parts[1])
	if market != "KR" && market != "US" || parts[2] == "" {
		return "", fmt.Errorf("invalid %s topic %q", expected, topic)
	}
	return market + ":" + parts[2], nil
}

func cloneSnapshot(snapshot MarketSnapshot) MarketSnapshot {
	snapshot.Asks = append([]QuoteLevel(nil), snapshot.Asks...)
	snapshot.Bids = append([]QuoteLevel(nil), snapshot.Bids...)
	snapshot.Trades = append([]Trade(nil), snapshot.Trades...)
	return snapshot
}

func (r *Realtime) setError(message string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastError = message
}

func (r *Realtime) clearError() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastError = ""
}

func nextDelay(delay time.Duration) time.Duration {
	if delay >= 30*time.Second {
		return 30 * time.Second
	}
	return delay * 2
}

func wait(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

type timestamp struct{ time.Time }

func (t *timestamp) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		t.Time = time.Time{}
		return nil
	}
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return err
	}
	t.Time = parsed
	return nil
}

type orderbookPayload struct {
	Timestamp timestamp    `json:"timestamp"`
	Currency  string       `json:"currency"`
	Asks      []QuoteLevel `json:"asks"`
	Bids      []QuoteLevel `json:"bids"`
}

type tradePayload struct {
	Price     string    `json:"price"`
	Volume    string    `json:"volume"`
	Timestamp timestamp `json:"timestamp"`
	Currency  string    `json:"currency"`
}
