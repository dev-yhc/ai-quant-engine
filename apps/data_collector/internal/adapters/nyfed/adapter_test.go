package nyfed

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDatasetCreatesContentAddressedName(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/vnd.ms-excel")
		_, _ = writer.Write([]byte("workbook"))
	}))
	defer server.Close()

	dataset, err := New(server.Client()).WithDatasetURL("hlw_r_star", server.URL).Dataset(context.Background(), "hlw_r_star")
	if err != nil {
		t.Fatal(err)
	}
	if len(dataset.Content) != len("workbook") || dataset.Name[:11] != "hlw_r_star:" {
		t.Fatalf("unexpected dataset: %#v", dataset)
	}
}
