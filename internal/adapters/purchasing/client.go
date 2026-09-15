package purchasing

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"erp/services/bi-service/internal/domain"
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

// OpenOrderQuantities sums item quantities per product across purchase orders
// that are APPROVED (approved but not yet received), so a storage plan doesn't
// suggest re-ordering stock that's already inbound.
func (c *Client) OpenOrderQuantities(ctx context.Context) (map[string]float64, error) {
	var orders []orderDTO
	if err := c.do(ctx, http.MethodGet, "/purchase-orders", nil, &orders); err != nil {
		return nil, err
	}
	out := map[string]float64{}
	for _, o := range orders {
		if o.Status != "APPROVED" {
			continue
		}
		for _, it := range o.Items {
			out[it.ProductID] += it.Quantity
		}
	}
	return out, nil
}

type createQuoteRequest struct {
	SupplierID string      `json:"supplier_id"`
	Notes      string      `json:"notes"`
	Items      []quoteItem `json:"items"`
}

type quoteItem struct {
	ProductID string  `json:"product_id"`
	Quantity  float64 `json:"quantity"`
	UnitPrice float64 `json:"unit_price"`
}

type quoteResponse struct {
	ID string `json:"id"`
}

func (c *Client) CreateQuote(ctx context.Context, supplierID string, items []domain.QuoteItem, notes string) (string, error) {
	body := createQuoteRequest{SupplierID: supplierID, Notes: notes}
	for _, it := range items {
		body.Items = append(body.Items, quoteItem{ProductID: it.ProductID, Quantity: it.Quantity, UnitPrice: it.UnitPrice})
	}
	var out quoteResponse
	if err := c.do(ctx, http.MethodPost, "/quotes", body, &out); err != nil {
		return "", err
	}
	return out.ID, nil
}

func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	var reader io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, reader)
	if err != nil {
		return err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if tok, ok := ctx.Value(AuthHeaderKey).(string); ok && tok != "" {
		req.Header.Set("Authorization", tok)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("purchasing: %s", strings.TrimSpace(string(b)))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
