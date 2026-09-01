// Package nyfed implements published NY Fed research workbook downloads.
package nyfed

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/yhc/quant-engine-go/domains/marketdata/domain"
)

const (
	ACMTermPremiumURL = "https://www.newyorkfed.org/medialibrary/media/research/data_indicators/ACMTermPremium.xlsx"
	HLWRStarURL       = "https://www.newyorkfed.org/medialibrary/media/research/economists/williams/data/Holston_Laubach_Williams_current_estimates.xlsx"
)

type Adapter struct {
	urls       map[string]string
	httpClient *http.Client
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
