// Heatmap intensity levels follow GitHub's quantile buckets: zero usage is
// empty, any nonzero usage is at least level 1, and the busiest day maps to
// the top level.
export const heatmapLevels = 4

export function heatLevel(value: number, max: number): number {
  if (value <= 0 || max <= 0) return 0
  return Math.min(heatmapLevels, Math.max(1, Math.ceil((value / max) * heatmapLevels)))
}
