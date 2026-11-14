// Package metrics computes every number the UI shows from one team's
// dataset: rates, percentiles, week buckets, classifications, thresholds,
// rankings, the facts tables' filter and search. Pure: no HTTP, no
// database, no config; package api reads the dataset and serves the result.
// The UI renders and formats, it computes nothing.
package metrics

import (
	"math"
	"slices"
	"time"
)

// Percentile is Postgres' percentile_cont: linear interpolation between the
// two nearest ranks, so the numbers match the README's queries. Nil on no
// values; values is left unsorted.
func Percentile(values []float64, p float64) *float64 {
	if len(values) == 0 {
		return nil
	}
	v := slices.Clone(values)
	slices.Sort(v)
	rank := p * float64(len(v)-1)
	lo := int(math.Floor(rank))
	hi := int(math.Ceil(rank))
	r := v[lo] + (v[hi]-v[lo])*(rank-float64(lo))
	return &r
}

// Median is the 50th percentile.
func Median(values []float64) *float64 { return Percentile(values, 0.5) }

// Spread is a sample's size, median and 90th percentile.
type Spread struct {
	N   int      `json:"n"`
	P50 *float64 `json:"p50"`
	P90 *float64 `json:"p90"`
}

func spread(values []float64) Spread {
	return Spread{N: len(values), P50: Median(values), P90: Percentile(values, 0.9)}
}

// between is the seconds from one instant to another; nil when either is
// missing.
func between(from, to *time.Time) *float64 {
	if from == nil || to == nil {
		return nil
	}
	s := to.Sub(*from).Seconds()
	return &s
}

// wait is between with a negative wait (approved before the ready flip,
// say) as unknown: the metrics leave those out.
func wait(from, to *time.Time) *float64 {
	s := between(from, to)
	if s == nil || *s < 0 {
		return nil
	}
	return s
}

// ratio is a/b; nil when b is 0.
func ratio(a, b int) *float64 {
	if b == 0 {
		return nil
	}
	r := float64(a) / float64(b)
	return &r
}

func ptr[T any](v T) *T { return &v }
