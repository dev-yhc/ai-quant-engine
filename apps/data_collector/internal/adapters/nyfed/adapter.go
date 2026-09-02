// Package nyfed downloads and normalizes published NY Fed research data.
package nyfed

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/yhc/quant-engine-go/domains/marketdata/domain"
)

const (
	ACMTermPremiumURL    = "https://www.newyorkfed.org/medialibrary/media/research/data_indicators/acmPlot_data.csv"
	HLWRStarURL          = "https://www.newyorkfed.org/medialibrary/media/research/economists/williams/data/Holston_Laubach_Williams_current_estimates.xlsx"
	ACMTermPremiumSeries = "ACM_TERM_PREMIUM"
	HLWRStarSeries       = "HLW_R_STAR"
)

type Adapter struct {
	urls       map[string]string
	httpClient *http.Client
}

// Observations turns a downloaded NY Fed dataset into the normalized series
// consumed by valuation-engine. Request-time valuation never reads source files.
func (a *Adapter) Observations(dataset domain.ResearchDataset) ([]domain.Observation, error) {
	name := dataset.Name
	if separator := strings.IndexByte(name, ':'); separator >= 0 {
		name = name[:separator]
	}
	switch name {
	case "acm_term_premium":
		return parseACMTermPremium(dataset.Content)
	case "hlw_r_star":
		return parseHLWRStar(dataset.Content)
	default:
		return nil, fmt.Errorf("unsupported NY Fed dataset %q", name)
	}
}

func parseACMTermPremium(content []byte) ([]domain.Observation, error) {
	records, err := csv.NewReader(bytes.NewReader(content)).ReadAll()
	if err != nil {
		return nil, fmt.Errorf("read ACM CSV: %w", err)
	}
	if len(records) < 2 {
		return nil, fmt.Errorf("ACM CSV contained no observations")
	}
	dateColumn, valueColumn := -1, -1
	for column, header := range records[0] {
		switch strings.TrimSpace(header) {
		case "RunDates":
			dateColumn = column
		case "TERMYld":
			valueColumn = column
		}
	}
	if dateColumn < 0 || valueColumn < 0 {
		return nil, fmt.Errorf("ACM CSV requires RunDates and TERMYld columns")
	}
	rows := make([][]string, 0, len(records)-1)
	for _, record := range records[1:] {
		if len(record) <= dateColumn || len(record) <= valueColumn {
			continue
		}
		rows = append(rows, []string{record[dateColumn], record[valueColumn]})
	}
	return parseObservationRows(rows, ACMTermPremiumSeries, "02-Jan-2006")
}

func parseHLWRStar(content []byte) ([]domain.Observation, error) {
	workbook, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		return nil, fmt.Errorf("open HLW workbook: %w", err)
	}
	parts := make(map[string]*zip.File, len(workbook.File))
	for _, file := range workbook.File {
		parts[file.Name] = file
	}
	workbookXML, err := readZipPart(parts, "xl/workbook.xml")
	if err != nil {
		return nil, err
	}
	var workbookDefinition struct {
		Sheets []struct {
			Name           string `xml:"name,attr"`
			RelationshipID string `xml:"id,attr"`
		} `xml:"sheets>sheet"`
	}
	if err := xml.Unmarshal(workbookXML, &workbookDefinition); err != nil {
		return nil, fmt.Errorf("parse HLW workbook definition: %w", err)
	}
	relationshipXML, err := readZipPart(parts, "xl/_rels/workbook.xml.rels")
	if err != nil {
		return nil, err
	}
	var relationships struct {
		Items []struct {
			ID     string `xml:"Id,attr"`
			Target string `xml:"Target,attr"`
		} `xml:"Relationship"`
	}
	if err := xml.Unmarshal(relationshipXML, &relationships); err != nil {
		return nil, fmt.Errorf("parse HLW workbook relationships: %w", err)
	}
	var relationshipID string
	for _, sheet := range workbookDefinition.Sheets {
		if sheet.Name == "HLW Estimates" {
			relationshipID = sheet.RelationshipID
			break
		}
	}
	if relationshipID == "" {
		return nil, fmt.Errorf("HLW Estimates sheet is missing")
	}
	var sheetTarget string
	for _, relationship := range relationships.Items {
		if relationship.ID == relationshipID {
			sheetTarget = relationship.Target
			break
		}
	}
	if sheetTarget == "" {
		return nil, fmt.Errorf("HLW Estimates sheet relationship is missing")
	}
	sheetPath := strings.TrimPrefix(path.Clean(path.Join("xl", sheetTarget)), "/")
	sheetXML, err := readZipPart(parts, sheetPath)
	if err != nil {
		return nil, err
	}
	sharedStrings, err := readSharedStrings(parts)
	if err != nil {
		return nil, err
	}
	var sheet struct {
		Rows []struct {
			Index int `xml:"r,attr"`
			Cells []struct {
				Reference string `xml:"r,attr"`
				Type      string `xml:"t,attr"`
				Value     string `xml:"v"`
			} `xml:"c"`
		} `xml:"sheetData>row"`
	}
	if err := xml.Unmarshal(sheetXML, &sheet); err != nil {
		return nil, fmt.Errorf("parse HLW Estimates sheet: %w", err)
	}
	cellValues := make(map[string]string)
	for _, row := range sheet.Rows {
		for _, cell := range row.Cells {
			value := cell.Value
			if cell.Type == "s" {
				index, parseErr := strconv.Atoi(value)
				if parseErr != nil || index < 0 || index >= len(sharedStrings) {
					return nil, fmt.Errorf("invalid HLW shared string index %q", value)
				}
				value = sharedStrings[index]
			}
			cellValues[cell.Reference] = value
		}
	}
	if strings.TrimSpace(cellValues["K5"]) != "Natural Rate (r*)" || strings.TrimSpace(cellValues["K6"]) != "US" {
		return nil, fmt.Errorf("HLW Estimates requires the US Natural Rate (r*) column")
	}
	values := make([][]string, 0, len(sheet.Rows))
	for _, row := range sheet.Rows {
		if row.Index < 7 {
			continue
		}
		dateSerial, serialErr := strconv.ParseFloat(cellValues[fmt.Sprintf("A%d", row.Index)], 64)
		if serialErr != nil {
			continue
		}
		date := time.Unix(int64((dateSerial-25569)*86400), 0).UTC()
		values = append(values, []string{date.Format("2006-01-02"), cellValues[fmt.Sprintf("K%d", row.Index)]})
	}
	return parseObservationRows(values, HLWRStarSeries, "2006-01-02")
}

func readZipPart(parts map[string]*zip.File, name string) ([]byte, error) {
	file, ok := parts[name]
	if !ok {
		return nil, fmt.Errorf("HLW workbook part %q is missing", name)
	}
	reader, err := file.Open()
	if err != nil {
		return nil, fmt.Errorf("open HLW workbook part %q: %w", name, err)
	}
	defer reader.Close()
	content, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read HLW workbook part %q: %w", name, err)
	}
	return content, nil
}

func readSharedStrings(parts map[string]*zip.File) ([]string, error) {
	if _, ok := parts["xl/sharedStrings.xml"]; !ok {
		return nil, nil
	}
	content, err := readZipPart(parts, "xl/sharedStrings.xml")
	if err != nil {
		return nil, err
	}
	var table struct {
		Items []struct {
			Text string `xml:"t"`
			Runs []struct {
				Text string `xml:"t"`
			} `xml:"r"`
		} `xml:"si"`
	}
	if err := xml.Unmarshal(content, &table); err != nil {
		return nil, fmt.Errorf("parse HLW shared strings: %w", err)
	}
	result := make([]string, 0, len(table.Items))
	for _, item := range table.Items {
		value := item.Text
		for _, run := range item.Runs {
			value += run.Text
		}
		result = append(result, value)
	}
	return result, nil
}

func parseObservationRows(rows [][]string, series string, preferredDateLayout string) ([]domain.Observation, error) {
	observations := make([]domain.Observation, 0, len(rows))
	for index, row := range rows {
		if len(row) < 2 || strings.TrimSpace(row[0]) == "" || strings.TrimSpace(row[1]) == "" || strings.EqualFold(strings.TrimSpace(row[1]), "NA") {
			continue
		}
		observedAt, err := parseDate(strings.TrimSpace(row[0]), preferredDateLayout)
		if err != nil {
			return nil, fmt.Errorf("parse %s row %d date: %w", series, index+1, err)
		}
		value, err := strconv.ParseFloat(strings.TrimSpace(row[1]), 64)
		if err != nil {
			return nil, fmt.Errorf("parse %s row %d value: %w", series, index+1, err)
		}
		observations = append(observations, domain.Observation{Series: series, ObservedAt: observedAt, Value: value, Provider: "ny_fed"})
	}
	if len(observations) == 0 {
		return nil, fmt.Errorf("%s dataset contained no observations", series)
	}
	return observations, nil
}

func parseDate(value string, preferredLayout string) (time.Time, error) {
	layouts := []string{preferredLayout, "2006-01-02", "2006-01-02 15:04:05", "1/2/06", "1/2/2006", "02-Jan-2006"}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported date %q", value)
}

func New(client *http.Client) *Adapter {
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	return &Adapter{urls: map[string]string{
		"acm_term_premium": ACMTermPremiumURL,
		"hlw_r_star":       HLWRStarURL,
	}, httpClient: client}
}

func (a *Adapter) WithDatasetURL(name, rawURL string) *Adapter {
	a.urls[name] = rawURL
	return a
}

func (a *Adapter) Dataset(ctx context.Context, name string) (domain.ResearchDataset, error) {
	rawURL, ok := a.urls[name]
	if !ok {
		return domain.ResearchDataset{}, fmt.Errorf("unsupported NY Fed dataset %q", name)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return domain.ResearchDataset{}, fmt.Errorf("create NY Fed request: %w", err)
	}
	response, err := a.httpClient.Do(request)
	if err != nil {
		return domain.ResearchDataset{}, fmt.Errorf("request NY Fed dataset %s: %w", name, err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return domain.ResearchDataset{}, fmt.Errorf("NY Fed dataset %s returned HTTP %d", name, response.StatusCode)
	}
	content, err := io.ReadAll(response.Body)
	if err != nil {
		return domain.ResearchDataset{}, fmt.Errorf("read NY Fed dataset %s: %w", name, err)
	}
	if len(content) == 0 {
		return domain.ResearchDataset{}, fmt.Errorf("NY Fed dataset %s was empty", name)
	}
	digest := sha256.Sum256(content)
	return domain.ResearchDataset{
		Name: fmt.Sprintf("%s:%x", name, digest), Provider: "ny_fed", Content: content,
		ContentType: response.Header.Get("Content-Type"),
	}, nil
}
