package userdash

import (
	"context"
)

// UsageRow is one per-model aggregate over the user's owned access keys.
type UsageRow struct {
	Model                string `json:"model"`
	RequestCount         int64  `json:"request_count"`
	EstimatedCostNanoUSD int64  `json:"estimated_cost_nano_usd"`
}

// QueryOwnedUsage aggregates usage_stats rows owned by the user between
// fromMS (inclusive) and toMS (exclusive), grouped by model.
func (s *Service) QueryOwnedUsage(ctx context.Context, userID uint, fromMS, toMS int64) ([]UsageRow, error) {
	rows := make([]UsageRow, 0)
	err := s.db.WithContext(ctx).Raw(`
		SELECT us.model,
		       COALESCE(SUM(us.request_count), 0) AS request_count,
		       COALESCE(SUM(us.estimated_cost_nano_usd), 0) AS estimated_cost_nano_usd
		FROM usage_stats us
		JOIN access_keys k ON k.id = us.access_key_id
		WHERE k.user_id = ?
		  AND us.bucket_start_ms >= ?
		  AND us.bucket_start_ms < ?
		GROUP BY us.model
		ORDER BY us.model ASC`, userID, fromMS, toMS).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}
