package main

import (
	"context"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
)

// loadOpenPositions reads tde_positions where position_status == "open".
// Used by /admin/close-all-positions.
func loadOpenPositions(ctx context.Context) ([]Position, error) {
	if mdb == nil {
		return nil, nil
	}
	c, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cur, err := mdb.Collection("tde_positions").Find(c, bson.M{"position_status": "open"})
	if err != nil {
		return nil, err
	}
	defer cur.Close(c)
	var out []Position
	if err := cur.All(c, &out); err != nil {
		return nil, err
	}
	return out, nil
}

type positionAgg struct {
	open    int
	closed  int
	openUSD float64
}

// aggregatePositionsByExchange groups tde_positions by exchange × status. Used
// by /api/v1/dashboard/stats. Returns an empty map (not an error) when Mongo
// is unavailable so the dashboard remains responsive.
func aggregatePositionsByExchange(ctx context.Context) (map[string]positionAgg, error) {
	out := map[string]positionAgg{}
	if mdb == nil {
		return out, nil
	}
	c, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cur, err := mdb.Collection("tde_positions").Find(c, bson.M{})
	if err != nil {
		return nil, err
	}
	defer cur.Close(c)
	for cur.Next(c) {
		var p Position
		if err := cur.Decode(&p); err != nil {
			continue
		}
		ex := strings.ToLower(p.Exchange)
		ag := out[ex]
		switch strings.ToLower(p.PositionStatus) {
		case "open":
			ag.open++
			ag.openUSD = round2(ag.openUSD + p.FilledQtyTotal*p.AvgFillPrice)
		case "closed":
			ag.closed++
		}
		out[ex] = ag
	}
	return out, nil
}

// sumRealizedPnL aggregates tde_trades.realized_pnl where created_at >= since.
func sumRealizedPnL(ctx context.Context, since time.Time) (float64, error) {
	if mdb == nil {
		return 0, nil
	}
	c, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	pipeline := []bson.M{
		{"$match": bson.M{
			"created_at":   bson.M{"$gte": since},
			"realized_pnl": bson.M{"$exists": true, "$ne": nil},
		}},
		{"$group": bson.M{
			"_id":   nil,
			"total": bson.M{"$sum": "$realized_pnl"},
		}},
	}
	cur, err := mdb.Collection("tde_trades").Aggregate(c, pipeline)
	if err != nil {
		return 0, err
	}
	defer cur.Close(c)
	if !cur.Next(c) {
		return 0, nil
	}
	var doc struct {
		Total float64 `bson:"total"`
	}
	if err := cur.Decode(&doc); err != nil {
		return 0, err
	}
	return doc.Total, nil
}
