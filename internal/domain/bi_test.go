package domain

import "testing"

func TestPurchaseLine_RoundsUpToWholePack(t *testing.T) {
	// Amazon sells banana only in 20kg boxes ("min_qty=1" of purchase UoM
	// "CX", registered price $110/box). Needing 71kg means buying 4 boxes
	// (80kg), not 71kg worth of a unit that doesn't exist.
	p := Product{
		PurchaseUoM: "CX",
		StockUoM:    "KG",
		Conversions: []UoMConversion{{FromUoM: "CX", ToUoM: "KG", Factor: 20}},
	}
	sp := &SupplierPrice{Price: 110, MinQty: 1}
	purchaseQty, unitPrice, ok := PurchaseLine(71, p, sp, 5.5)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if purchaseQty != 4 {
		t.Errorf("purchaseQty = %v, want 4", purchaseQty)
	}
	if unitPrice != 110 {
		t.Errorf("unitPrice = %v, want 110", unitPrice)
	}
}

func TestPurchaseLine_ExactMultipleDoesNotOverRound(t *testing.T) {
	p := Product{
		PurchaseUoM: "CX",
		StockUoM:    "KG",
		Conversions: []UoMConversion{{FromUoM: "CX", ToUoM: "KG", Factor: 20}},
	}
	sp := &SupplierPrice{Price: 110, MinQty: 1}
	purchaseQty, _, ok := PurchaseLine(80, p, sp, 5.5)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if purchaseQty != 4 {
		t.Errorf("purchaseQty = %v, want 4 (exact multiple, no extra pack)", purchaseQty)
	}
}

func TestPurchaseLine_FractionalPackSize(t *testing.T) {
	// Supplier B: $12 per 1.5 CX pack, 1 CX = 20kg -> pack = 30kg.
	p := Product{
		PurchaseUoM: "CX",
		StockUoM:    "KG",
		Conversions: []UoMConversion{{FromUoM: "CX", ToUoM: "KG", Factor: 20}},
	}
	sp := &SupplierPrice{Price: 12, MinQty: 1.5}
	purchaseQty, unitPrice, ok := PurchaseLine(31, p, sp, 0.4)
	if !ok {
		t.Fatal("expected ok=true")
	}
	// 31kg needs ceil(31/30)=2 packs -> 2*1.5=3 CX, at $12/1.5=$8/CX.
	if purchaseQty != 3 {
		t.Errorf("purchaseQty = %v, want 3", purchaseQty)
	}
	if unitPrice != 8 {
		t.Errorf("unitPrice = %v, want 8", unitPrice)
	}
}

func TestPurchaseLine_NoRegisteredPriceFallsBackToUnitCost(t *testing.T) {
	p := Product{
		PurchaseUoM: "CX",
		StockUoM:    "KG",
		Conversions: []UoMConversion{{FromUoM: "CX", ToUoM: "KG", Factor: 20}},
	}
	purchaseQty, unitPrice, ok := PurchaseLine(71, p, nil, 5.5)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if purchaseQty != 4 {
		t.Errorf("purchaseQty = %v, want 4 (rounds up to whole purchase units)", purchaseQty)
	}
	if unitPrice != 110 {
		t.Errorf("unitPrice = %v, want 110 (5.5/kg * 20kg/CX)", unitPrice)
	}
}

func TestPurchaseLine_NoConversionAvailable(t *testing.T) {
	p := Product{PurchaseUoM: "CX", StockUoM: "KG"}
	_, _, ok := PurchaseLine(71, p, nil, 5.5)
	if ok {
		t.Fatal("expected ok=false when purchase UoM can't convert to stock UoM")
	}
}

func TestPurchaseLine_SameUoMPassesThrough(t *testing.T) {
	p := Product{PurchaseUoM: "UN", StockUoM: "UN"}
	sp := &SupplierPrice{Price: 100, MinQty: 4}
	purchaseQty, unitPrice, ok := PurchaseLine(27, p, sp, 0)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if purchaseQty != 27 {
		t.Errorf("purchaseQty = %v, want 27 (same UoM, no packaging)", purchaseQty)
	}
	if unitPrice != 25 {
		t.Errorf("unitPrice = %v, want 25 (100/4)", unitPrice)
	}
}

func TestFinancialLineFor(t *testing.T) {
	f := ForecastEntry{Kind: ForecastKindKit, TargetID: "k1", Code: "000002", Name: "3p", EffectiveWeeklyQty: 4}
	line := FinancialLineFor(f, 50, 30) // sells for $50, costs $30 to assemble
	if line.WeeklyRevenue != 200 {
		t.Errorf("WeeklyRevenue = %v, want 200", line.WeeklyRevenue)
	}
	if line.WeeklyCost != 120 {
		t.Errorf("WeeklyCost = %v, want 120", line.WeeklyCost)
	}
	if line.WeeklyProfit != 80 {
		t.Errorf("WeeklyProfit = %v, want 80", line.WeeklyProfit)
	}
	if line.MarginPercent != 40 {
		t.Errorf("MarginPercent = %v, want 40", line.MarginPercent)
	}
}

func TestFinancialLineFor_NoRevenueHasZeroMargin(t *testing.T) {
	f := ForecastEntry{EffectiveWeeklyQty: 10}
	line := FinancialLineFor(f, 0, 5)
	if line.MarginPercent != 0 {
		t.Errorf("MarginPercent = %v, want 0 when there's no revenue", line.MarginPercent)
	}
	if line.WeeklyProfit != -50 {
		t.Errorf("WeeklyProfit = %v, want -50 (all cost, no revenue)", line.WeeklyProfit)
	}
}
