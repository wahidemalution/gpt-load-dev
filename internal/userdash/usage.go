package userdash

import (
	"context"
)

// UsageRow is one per-model aggregate over the user's owned access keys.
type UsageRow struct {
	Model                string `json:"model"`
	RequestCount         int64  `json:"request_count"`
	EstimatedCostNanoUSD int64  `json:"estimated_cost_nano_usd"`
	TokensIn             int64  `json:"tokens_in"`
	TokensOut            int64  `json:"tokens_out"`
	CacheReadTokens      int64  `json:"cache_read_tokens"`
	CacheWriteTokens     int64  `json:"cache_write_tokens"`
	AvgTTFTMs            *int64 `json:"avg_ttft_ms"`
	AvgLatencyMs         int64  `json:"avg_latency_ms"`
}

// mergeUsageLatency folds per-model latency rows into the token/cost rows,
// matching on model name. Latency-only models (no usage_stats row) are appended
// so the response reflects every model seen in the period.
func mergeUsageLatency(rows []UsageRow, latency []UsageLatencyRow) []UsageRow {
	index := make(map[string]int, len(rows))
	for i := range rows {
		index[rows[i].Model] = i
	}
	for _, l := range latency {
		if i, ok := index[l.Model]; ok {
			rows[i].AvgTTFTMs = l.AvgTTFTMs
			rows[i].AvgLatencyMs = l.AvgLatencyMs
			continue
		}
		index[l.Model] = len(rows)
		rows = append(rows, UsageRow{
			Model:        l.Model,
			AvgTTFTMs:    l.AvgTTFTMs,
			AvgLatencyMs: l.AvgLatencyMs,
		})
	}
	return rows
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
		       COALESCE(SUM(us.estimated_cost_nano_usd), 0) AS estimated_cost_nano_usd,
		       COALESCE(SUM(us.uncached_input_tokens), 0) AS tokens_in,
		       COALESCE(SUM(us.output_tokens), 0) AS tokens_out,
		       COALESCE(SUM(us.cache_read_tokens), 0) AS cache_read_tokens,
		       COALESCE(SUM(
		           us.cache_write_5m_tokens
		           + us.cache_write_1h_tokens
		           + us.cache_write_unknown_tokens
		       ), 0) AS cache_write_tokens
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

// UsageLatencyRow is one per-model latency aggregate over the user's owned
// access keys, derived from request logs. AvgTTFTMs is nil when the period has
// no streaming samples carrying a first-response measurement.
type UsageLatencyRow struct {
	Model        string `json:"model"`
	AvgTTFTMs    *int64 `json:"avg_ttft_ms"`
	AvgLatencyMs int64  `json:"avg_latency_ms"`
}

// QueryOwnedLatency averages first-response and total duration per model across
// the user's owned access keys between fromMS (inclusive) and toMS (exclusive).
func (s *Service) QueryOwnedLatency(ctx context.Context, userID uint, fromMS, toMS int64) ([]UsageLatencyRow, error) {
	rows := make([]UsageLatencyRow, 0)
	err := s.db.WithContext(ctx).Raw(`
		SELECT rl.client_model AS model,
		       ROUND(AVG(rl.first_response_ms)) AS avg_ttft_ms,
		       COALESCE(ROUND(AVG(rl.duration_ms)), 0) AS avg_latency_ms
		FROM request_logs rl
		JOIN access_keys k ON k.id = rl.access_key_id
		WHERE k.user_id = ?
		  AND rl.completed_at_ms >= ?
		  AND rl.completed_at_ms < ?
		GROUP BY rl.client_model
		ORDER BY rl.client_model ASC`, userID, fromMS, toMS).Scan(&rows).Error
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
