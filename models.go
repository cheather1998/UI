package main

import "time"

// Local copies of the small subset of TES schemas we need to read.
// We only redeclare the fields we actually consume; extra fields in the source
// docs are ignored on decode. This keeps the control service decoupled from
// the TES module entirely (no Go imports across module boundaries).

// TradeData mirrors the JSON written by TES at tde:order:json:v1:* (Redis)
// and tde_orders / tde_trades (Mongo).
type TradeData struct {
	OrderID       string    `bson:"order_id"               json:"order_id"`
	Exchange      string    `bson:"exchange"               json:"exchange"`
	Market        string    `bson:"market"                 json:"market"`
	Symbol        string    `bson:"symbol"                 json:"symbol"`
	Status        string    `bson:"status"                 json:"status"`
	Side          string    `bson:"side"                   json:"side"`
	OpportunityID string    `bson:"opportunity_id"         json:"opportunity_id"`
	RealizedPnL   *float64  `bson:"realized_pnl,omitempty" json:"realized_pnl,omitempty"`
	CreatedAt     time.Time `bson:"created_at"             json:"created_at"`
}

// Position mirrors the doc shape in tde_positions (Mongo).
type Position struct {
	OpportunityID  string  `bson:"opportunity_id"   json:"opportunity_id"`
	Leg            string  `bson:"leg"              json:"leg"`
	Exchange       string  `bson:"exchange"         json:"exchange"`
	Market         string  `bson:"market"           json:"market"`
	Symbol         string  `bson:"symbol"           json:"symbol"`
	OrderID        string  `bson:"order_id"         json:"order_id"`
	PositionStatus string  `bson:"position_status"  json:"position_status"`
	FilledQtyTotal float64 `bson:"filled_qty_total" json:"filled_qty_total"`
	AvgFillPrice   float64 `bson:"avg_fill_price"   json:"avg_fill_price"`
}

// OrderActionFailure is returned (per failed order) by /admin/cancel-all-orders
// and /admin/close-all-positions.
type OrderActionFailure struct {
	Exchange string `json:"exchange"`
	Market   string `json:"market"`
	OrderID  string `json:"order_id"`
	Error    string `json:"error"`
}
