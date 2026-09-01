package fred

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestObservationsNormalizesValuesAndSkipsMissing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if got := request.URL.Query().Get("series_id"); got != "DGS10" {
			t.Fatalf("series_id = %q", got)
		}
		if got := request.URL.Query().Get("api_key"); got != "test-key" {
			t.Fatalf("api key = %q", got)
		}
		_, _ = io.WriteString(writer, `{"observations":[{"date":"2026-08-28","value":"4.20"},{"date":"2026-08-29","value":"."}]}`)
	}))
	defer server.Close()

	adapter, err := New("test-key", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	observations, err := adapter.WithObservationsURL(server.URL).Observations(context.Background(), []string{"DGS10"})
	if err != nil {
		t.Fatal(err)
	}
	if len(observations) != 1 || observations[0].Value != 4.2 {
		t.Fatalf("unexpected observations: %#v", observations)
	}
}
