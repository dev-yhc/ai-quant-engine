package toss

import (
	"encoding/json"
	"testing"

	"github.com/yhc/quant-engine-go/apps/trading-engine/internal/domain"
)

func TestMarketSnapshotChoosesInsideQuotesRegardlessOfPayloadOrder(t *testing.T) {
	snapshot := MarketSnapshot{
		Bids: []QuoteLevel{{Price: "100", Volume: "1"}, {Price: "101", Volume: "2"}},
		Asks: []QuoteLevel{{Price: "103", Volume: "1"}, {Price: "102", Volume: "2"}},
	}
	if level, ok := snapshot.PassiveLimitPrice(domain.Buy); !ok || level.Price != "101" {
		t.Fatalf("buy quote = %#v, %v", level, ok)
	}
	if level, ok := snapshot.PassiveLimitPrice(domain.Sell); !ok || level.Price != "102" {
		t.Fatalf("sell quote = %#v, %v", level, ok)
	}
}

func TestRealtimeAppliesMarketAndPersonalOrderEvents(t *testing.T) {
	realtime := NewRealtime(nil)
	orderbook := json.RawMessage(`{"type":"message","topic":"orderbook:kr:005930","data":{"timestamp":"2026-09-02T09:00:00+09:00","currency":"KRW","asks":[{"price":"70100","volume":"2"}],"bids":[{"price":"70000","volume":"3"}]}}`)
	if err := realtime.applyFrame(orderbook); err != nil {
		t.Fatal(err)
	}
	trade := json.RawMessage(`{"type":"message","topic":"trade:kr:005930","data":{"price":"70000","volume":"10","timestamp":"2026-09-02T09:00:01+09:00","currency":"KRW"}}`)
	if err := realtime.applyFrame(trade); err != nil {
		t.Fatal(err)
	}
	snapshot, ok := realtime.Snapshot("KR:005930")
	bestBid, hasBid := snapshot.BestBid()
	if !ok || len(snapshot.Trades) != 1 || !hasBid || bestBid.Price != "70000" {
		t.Fatalf("snapshot = %#v, %v", snapshot, ok)
	}
	event := json.RawMessage(`{"type":"message","topic":"personal:order:7","data":{"event":"FILL","accountSeq":"7","order":{"orderId":"broker-1","symbol":"005930","side":"BUY","orderType":"LIMIT","status":"FILLED","quantity":"1","currency":"KRW","orderedAt":"2026-09-02T09:00:00+09:00","execution":{"filledQuantity":"1"}}}}`)
	if err := realtime.applyFrame(event); err != nil {
		t.Fatal(err)
	}
	order, ok := realtime.Order("broker-1")
	if !ok || order.Event != "FILL" || order.Order.Status != "FILLED" || order.Order.Execution.FilledQuantity != "1" {
		t.Fatalf("order = %#v, %v", order, ok)
	}
}

func TestSubscriptionDeclarationKeepsAllChannelsInOneFullReplacePayload(t *testing.T) {
	declaration := subscriptionDeclaration([]string{"US:AAPL", "KR:005930"}, 7)
	encoded, err := json.Marshal(declaration)
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"codes":["005930"],"type":"trade:kr"},{"codes":["AAPL"],"type":"trade:us"},{"codes":["005930"],"type":"orderbook:kr"},{"codes":["AAPL"],"type":"orderbook:us"},{"codes":["7"],"type":"personal:order"}]`
	if string(encoded) != want {
		t.Fatalf("declaration = %s", encoded)
	}
}

func TestRealtimeRejectsMoreThanFortyNineInstruments(t *testing.T) {
	instruments := make([]string, 50)
	for i := range instruments {
		instruments[i] = "US:TEST" + string(rune('A'+i))
	}
	if err := NewRealtime(nil).Run(t.Context(), instruments); err == nil {
		t.Fatal("expected topic limit error")
	}
}
