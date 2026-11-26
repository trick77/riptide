package metrics

import (
	"math"
	"slices"
)

// ---- A strip's scale --------------------------------------------------------

// logScaleFactor: positive values spanning this factor or more sit on a log
// scale, so one repo shipping daily does not squeeze every other one into a
// single spot at the slow end.
const logScaleFactor = 8.0

// maxTicks is the most labelled values a strip's axis carries.
const maxTicks = 4

// niceSteps are the round values a unit's ticks fall on, ascending.
func niceSteps(unit string) []float64 {
	switch unit {
	case UnitSeconds:
		return []float64{30, 60, 120, 300, 600, 900, 1800, 3600, 7200, 4 * 3600, 8 * 3600, 86400, 2 * 86400,
			7 * 86400}
	case UnitDays, UnitWorkingDays:
		return []float64{0.25, 0.5, 1, 2, 3, 5, 7, 14, 21, 28, 42, 56, 91, 182}
	case UnitShare:
		return []float64{0.05, 0.1, 0.2, 0.25, 0.5}
	}
	var out []float64
	for e := -2; e <= 7; e++ {
		p := math.Pow(10, float64(e))
		out = append(out, p, 2*p, 5*p)
	}
	return out
}

// stripScale fits a strip's axis to values (each repo's) and mark (the
// team's threshold, nil without one), in unit; higher puts the larger values
// at the left, the better end. A share runs to 100 % from the nearest 10 %
// below its lowest value (at most 90 %); other values sit on a log scale
// when they span logScaleFactor, else on a linear one from near the lowest,
// both padded so no dot sits on the edge. Every value equal: the scale
// centres on it.
func stripScale(values []float64, mark *float64, unit string, higher bool) StripScale {
	all := slices.Clone(values)
	if mark != nil {
		all = append(all, *mark)
	}
	var s StripScale
	switch unit {
	case UnitShare:
		lo := 1.0
		for _, v := range all {
			lo = min(lo, v)
		}
		s.Lo, s.Hi = min(0.9, math.Floor(lo*10)/10), 1
		s.Ticks = linearTicks(s.Lo, s.Hi, unit)
	default:
		var pos []float64
		for _, v := range all {
			if v > 0 {
				pos = append(pos, v)
			}
		}
		if len(pos) == 0 {
			s.Lo, s.Hi = 0, 1
			s.Ticks = linearTicks(s.Lo, s.Hi, unit)
			break
		}
		lo, hi := slices.Min(pos), slices.Max(pos)
		if hi/lo >= logScaleFactor {
			s.Log, s.Lo, s.Hi = true, lo/1.25, hi*1.25
			s.Ticks = logTicks(s.Lo, s.Hi, unit)
			break
		}
		lo = slices.Min(all)
		pad := (hi - lo) * 0.15
		if hi == lo {
			pad = hi * 0.5
		}
		s.Lo, s.Hi = max(0, lo-pad), hi+pad
		s.Ticks = linearTicks(s.Lo, s.Hi, unit)
	}
	for i := range s.Ticks {
		s.Ticks[i].X, _ = s.position(s.Ticks[i].Value, higher)
	}
	return s
}

// position is v's place on the scale, 0..1 with the better end at 0 (the
// larger values when higher); clamped: v lies outside it and sits at its
// end (a 0 on a log scale).
func (s StripScale) position(v float64, higher bool) (float64, bool) {
	var t float64
	switch {
	case s.Log && v <= 0:
		t = -1
	case s.Log:
		t = (math.Log(v) - math.Log(s.Lo)) / (math.Log(s.Hi) - math.Log(s.Lo))
	case s.Hi > s.Lo:
		t = (v - s.Lo) / (s.Hi - s.Lo)
	default:
		t = 0.5
	}
	clamped := t < 0 || t > 1
	t = min(max(t, 0), 1)
	if higher {
		t = 1 - t
	}
	return math.Round(t*10000) / 10000, clamped
}

// linearTicks are multiples of the smallest nice step of unit that leaves at
// most maxTicks of them in [lo, hi].
func linearTicks(lo, hi float64, unit string) []StripTick {
	steps := niceSteps(unit)
	step := steps[len(steps)-1]
	for _, st := range steps {
		if (hi-lo)/st < maxTicks {
			step = st
			break
		}
	}
	out := []StripTick{}
	for v := math.Ceil(lo/step-1e-9) * step; v <= hi+1e-9; v += step {
		// +0, never -0 (lo/step just below zero rounds up to it).
		out = append(out, StripTick{Value: math.Round(v*1e6)/1e6 + 0})
	}
	return out
}

// logTicks are the nice values of unit in [lo, hi], at most maxTicks,
// spread evenly over the scale.
func logTicks(lo, hi float64, unit string) []StripTick {
	var in []float64
	for _, v := range niceSteps(unit) {
		if v >= lo && v <= hi {
			in = append(in, v)
		}
	}
	if len(in) > maxTicks {
		var pick []float64
		for i := range maxTicks {
			v := in[int(math.Round(float64(i)*float64(len(in)-1)/float64(maxTicks-1)))]
			if !slices.Contains(pick, v) {
				pick = append(pick, v)
			}
		}
		in = pick
	}
	out := []StripTick{}
	for _, v := range in {
		out = append(out, StripTick{Value: v})
	}
	return out
}
