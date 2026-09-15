package sales

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type ctxKey string

const AuthHeaderKey ctxKey = "authorization"

type Client struct {
	base string
	http *http.Client
}

func New(base string) *Client {
	return &Client{
		base: strings.TrimRight(base, "/"),
		http: &http.Client{Timeout: 10 * time.Second},
	}
}

type orderDTO struct {
	Status string `json:"status"`
	Items  []struct {
		ProductID string  `json:"product_id"`
		Quantity  float64 `json:"quantity"`
	} `json:"items"`
}

// OrderQuantities sums item quantities per product across all non-cancelled
// sales orders created within [from, to].
func (c *Client) OrderQuantities(ctx context.Context, from, to time.Time) (map[string]float64, error) {
	q := url.Values{}
	q.Set("from", from.UTC().Format(time.RFC3339))
	q.Set("to", to.UTC().Format(time.RFC3339))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/sales-orders?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	if tok, ok := ctx.Value(AuthHeaderKey).(string); ok && tok != "" {
		req.Header.Set("Authorization", tok)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("sales: %s", strings.TrimSpace(string(b)))
	}
	var orders []orderDTO
	if err := json.NewDecoder(resp.Body).Decode(&orders); err != nil {
		return nil, err
	}
	out := map[string]float64{}
	for _, o := range orders {
		if o.Status == "CANCELLED" {
			continue
		}
		for _, it := range o.Items {
			out[it.ProductID] += it.Quantity
		}
	}
	return out, nil
}
