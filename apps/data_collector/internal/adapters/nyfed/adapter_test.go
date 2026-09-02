package nyfed

import (
	"archive/zip"
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yhc/quant-engine-go/domains/marketdata/domain"
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

func TestParseObservationRowsNormalizesACMTermPremium(t *testing.T) {
	observations, err := parseObservationRows([][]string{{"28-Aug-2026", "0.7280357031700317"}}, ACMTermPremiumSeries, "02-Jan-2006")
	if err != nil {
		t.Fatal(err)
	}
	if len(observations) != 1 || observations[0].Series != ACMTermPremiumSeries || observations[0].Provider != "ny_fed" || observations[0].ObservedAt.Format("2006-01-02") != "2026-08-28" || observations[0].Value != 0.7280357031700317 {
		t.Fatalf("unexpected observation: %#v", observations)
	}
}

func TestParseACMTermPremiumCSV(t *testing.T) {
	content := []byte("RunDates,TERMYld,ACMFITYld,GSWYld\n31-Jul-2026,0.837552069026338,4.82299845150996,4.83483964203813\n")
	observations, err := parseACMTermPremium(content)
	if err != nil {
		t.Fatal(err)
	}
	if len(observations) != 1 || observations[0].Series != ACMTermPremiumSeries || observations[0].ObservedAt.Format("2006-01-02") != "2026-07-31" || observations[0].Value != 0.837552069026338 {
		t.Fatalf("unexpected observation: %#v", observations)
	}
}

func TestParseHLWRStar(t *testing.T) {
	var content bytes.Buffer
	writer := zip.NewWriter(&content)
	writeZipTestPart(t, writer, "xl/workbook.xml", `<workbook xmlns:r="r"><sheets><sheet name="HLW Estimates" r:id="rId1"/></sheets></workbook>`)
	writeZipTestPart(t, writer, "xl/_rels/workbook.xml.rels", `<Relationships><Relationship Id="rId1" Target="worksheets/sheet1.xml"/></Relationships>`)
	writeZipTestPart(t, writer, "xl/sharedStrings.xml", `<sst><si><t>Natural Rate (r*)</t></si><si><t>US</t></si></sst>`)
	writeZipTestPart(t, writer, "xl/worksheets/sheet1.xml", `<worksheet><sheetData><row r="5"><c r="K5" t="s"><v>0</v></c></row><row r="6"><c r="K6" t="s"><v>1</v></c></row><row r="7"><c r="A7"><v>46113</v></c><c r="K7"><v>1.00864835850403</v></c></row></sheetData></worksheet>`)
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	observations, err := parseHLWRStar(content.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if len(observations) != 1 || observations[0].Series != HLWRStarSeries || observations[0].ObservedAt.Format("2006-01-02") != "2026-04-01" || observations[0].Value != 1.00864835850403 {
		t.Fatalf("unexpected observation: %#v", observations)
	}
}

func writeZipTestPart(t *testing.T, writer *zip.Writer, name, content string) {
	t.Helper()
	part, err := writer.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
}

func TestObservationsRejectsUnknownDataset(t *testing.T) {
	_, err := New(nil).Observations(domain.ResearchDataset{Name: "unknown:hash"})
	if err == nil {
		t.Fatal("expected unsupported dataset error")
	}
}
