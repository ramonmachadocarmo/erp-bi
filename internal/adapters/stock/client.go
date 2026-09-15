package stock

import (
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

type uomConversionDTO struct {
	FromUoM string  `json:"from_uom"`
	ToUoM   string  `json:"to_uom"`
	Factor  float64 `json:"factor"`
}

type productDTO struct {
	ID             string             `json:"id"`
	SKU            string             `json:"sku"`
	Name           string             `json:"name"`
	Kind           string             `json:"kind"`
	PurchasePrice  float64            `json:"purchase_price"`
	SalePrice      float64            `json:"sale_price"`
	PurchaseUoM    string             `json:"purchase_uom"`
	StockUoM       string             `json:"stock_uom"`
	SaleUoM        string             `json:"sale_uom"`
	UoMConversions []uomConversionDTO `json:"uom_conversions"`
}

type balanceDTO struct {
	ProductID         string  `json:"product_id"`
	WarehouseID       string  `json:"warehouse_id"`
	QuantityAvailable float64 `json:"quantity_available"`
}

type assemblyItemDTO struct {
	ProductID string  `json:"product_id"`
	Quantity  float64 `json:"quantity"`
}

type assemblyDTO struct {
	ID        string            `json:"id"`
	Code      string            `json:"code"`
	Name      string            `json:"name"`
	ProductID string            `json:"product_id"`
	Cost      float64           `json:"cost"`
	Items     []assemblyItemDTO `json:"items"`
}

func (c *Client) Products(ctx context.Context) ([]domain.Product, error) {
	var out []productDTO
	if err := c.get(ctx, "/products", &out); err != nil {
		return nil, err
	}
	products := make([]domain.Product, 0, len(out))
	for _, p := range out {
		convs := make([]domain.UoMConversion, 0, len(p.UoMConversions))
		for _, c := range p.UoMConversions {
			convs = append(convs, domain.UoMConversion{FromUoM: c.FromUoM, ToUoM: c.ToUoM, Factor: c.Factor})
		}
		products = append(products, domain.Product{
			ID: p.ID, SKU: p.SKU, Name: p.Name, Kind: p.Kind, PurchasePrice: p.PurchasePrice, SalePrice: p.SalePrice,
			PurchaseUoM: p.PurchaseUoM, StockUoM: p.StockUoM, SaleUoM: p.SaleUoM, Conversions: convs,
		})
	}
	return products, nil
}

func (c *Client) Balances(ctx context.Context) ([]domain.Balance, error) {
	var out []balanceDTO
	if err := c.get(ctx, "/balances", &out); err != nil {
		return nil, err
	}
	balances := make([]domain.Balance, 0, len(out))
	for _, b := range out {
		balances = append(balances, domain.Balance{ProductID: b.ProductID, WarehouseID: b.WarehouseID, QuantityAvailable: b.QuantityAvailable})
	}
	return balances, nil
}

func (c *Client) Assemblies(ctx context.Context) ([]domain.Assembly, error) {
	var out []assemblyDTO
	if err := c.get(ctx, "/assemblies", &out); err != nil {
		return nil, err
	}
	assemblies := make([]domain.Assembly, 0, len(out))
	for _, a := range out {
		items := make([]domain.AssemblyItem, 0, len(a.Items))
		for _, it := range a.Items {
			items = append(items, domain.AssemblyItem{ProductID: it.ProductID, Quantity: it.Quantity})
		}
		assemblies = append(assemblies, domain.Assembly{ID: a.ID, Code: a.Code, Name: a.Name, ProductID: a.ProductID, Cost: a.Cost, Items: items})
	}
	return assemblies, nil
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return err
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
		return fmt.Errorf("stock: %s", strings.TrimSpace(string(b)))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
