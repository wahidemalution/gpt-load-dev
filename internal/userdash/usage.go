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

// DailyTokenRow is one UTC calendar day of total token usage across the user's
// owned access keys. DayMS is the UTC-midnight bucket start in milliseconds.
type DailyTokenRow struct {
	DayMS       int64 `json:"day_ms"`
	TotalTokens int64 `json:"total_tokens"`
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

// QueryOwnedDailyTokens sums total tokens per UTC calendar day over the user's
// owned access keys between fromMS (inclusive) and toMS (exclusive). Rows are
// ordered by day ascending; days without usage are absent.
func (s *Service) QueryOwnedDailyTokens(ctx context.Context, userID uint, fromMS, toMS int64) ([]DailyTokenRow, error) {
	rows := make([]DailyTokenRow, 0)
	err := s.db.WithContext(ctx).Raw(`
		SELECT (us.bucket_start_ms / 86400000) * 86400000 AS day_ms,
		       COALESCE(SUM(
		           us.uncached_input_tokens
		           + us.output_tokens
		           + us.cache_read_tokens
		           + us.cache_write_5m_tokens
		           + us.cache_write_1h_tokens
		           + us.cache_write_unknown_tokens
		       ), 0) AS total_tokens
		FROM usage_stats us
		JOIN access_keys k ON k.id = us.access_key_id
		WHERE k.user_id = ?
		  AND us.bucket_start_ms >= ?
		  AND us.bucket_start_ms < ?
		GROUP BY day_ms
		ORDER BY day_ms ASC`, userID, fromMS, toMS).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}
