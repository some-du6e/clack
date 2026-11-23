package chart

import (
	"math"
	"strconv"
	"strings"
)

// ECharts 6.1.0's util/number.ts and the value axis' Interval scale, for
// the axis Slack's charts have: min and max from the data, zero included
// (scale: false), splitNumber 5.

// getPrecision is number.getPrecision: the decimal places val needs.
func getPrecision(val float64) int {
	if math.IsNaN(val) {
		return 0
	}
	if val > 1e-14 {
		e := 1.0
		for i := 0; i < 15; i, e = i+1, e*10 {
			if math.Round(val*e)/e == val {
				return i
			}
		}
	}
	// getPrecisionSafe: from the number's string
	s := strings.ToLower(JSNumber(val))
	exp := 0
	sig := len(s)
	if i := strings.IndexByte(s, 'e'); i > 0 {
		exp, _ = strconv.Atoi(s[i+1:])
		sig = i
	}
	dec := 0
	if d := strings.IndexByte(s, '.'); d >= 0 {
		dec = sig - 1 - d
	}
	return max(0, dec-exp)
}

// roundTo is number.round: +(+x).toFixed(precision).
func roundTo(x float64, precision int) float64 {
	precision = min(max(0, precision), 20)
	f, _ := strconv.ParseFloat(strconv.FormatFloat(x, 'f', precision, 64), 64)
	return f
}

// quantityExponent is number.quantityExponent.
func quantityExponent(val float64) int {
	if val == 0 {
		return 0
	}
	exp := int(math.Floor(math.Log(val) / math.Ln10))
	if val/math.Pow10(exp) >= 10 {
		exp++
	}
	return exp
}

// nice is number.nice: 1, 2, 3, 5 or 10 times a power of ten, rounded
// (NICE_MODE_ROUND) or ceiled.
func nice(val float64, round bool) float64 {
	exponent := quantityExponent(val)
	exp10 := math.Pow10(exponent)
	f := val / exp10
	var nf float64
	if round {
		switch {
		case f < 1.5:
			nf = 1
		case f < 2.5:
			nf = 2
		case f < 4:
			nf = 3
		case f < 7:
			nf = 5
		default:
			nf = 10
		}
	} else {
		switch {
		case f < 1:
			nf = 1
		case f < 2:
			nf = 2
		case f < 3:
			nf = 3
		case f < 5:
			nf = 5
		default:
			nf = 10
		}
	}
	val = nf * exp10
	if exponent >= -20 {
		p := 0
		if exponent < 0 {
			p = -exponent
		}
		return roundTo(val, p)
	}
	return val
}

// valueScale is the value axis' scale.
type valueScale struct {
	ext      [2]float64
	interval float64
	ticks    []float64
	blank    bool // no value at all: no labels
}

// newValueScale is ScaleRawExtentInfo + scaleCalcNice over vs (NaN: none).
// startValue: a bar series asks for its baseline (0) in the extent.
func newValueScale(vs []float64, startValue bool) valueScale {
	mn, mx := math.Inf(1), math.Inf(-1)
	for _, v := range vs {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			continue
		}
		mn = math.Min(mn, v)
		mx = math.Max(mx, v)
	}
	var s valueScale
	s.blank = !(mn <= mx)
	ext := [2]float64{math.NaN(), math.NaN()}
	if !s.blank {
		ext = [2]float64{mn, mx} // boundaryGap [0, 0]
		// needIncludeZero
		if ext[0] > 0 && ext[1] > 0 {
			ext[0] = 0
		}
		if ext[0] < 0 && ext[1] < 0 {
			ext[1] = 0
		}
		if startValue {
			if 0 < ext[0] {
				ext[0] = 0
			} else if 0 > ext[1] {
				ext[1] = 0
			}
		}
	}
	// intervalScaleEnsureValidExtent
	if ext[0] == ext[1] {
		if ext[0] != 0 {
			e := math.Abs(ext[0])
			ext[1] += e / 2
			ext[0] -= e / 2
		} else {
			ext[1] = 1
		}
	}
	if math.IsNaN(ext[0]) || math.IsNaN(ext[1]) || math.IsInf(ext[0], 0) || math.IsInf(ext[1], 0) {
		ext = [2]float64{0, 1}
	}
	span := ext[1] - ext[0]
	interval := nice(span/5, true)
	prec := getPrecision(interval) + 2
	ext = [2]float64{roundTo(math.Floor(ext[0]/interval)*interval, prec), roundTo(math.Ceil(ext[1]/interval)*interval, prec)}
	s.ext, s.interval = ext, interval
	// Interval.getTicks over the niced extent: no ticks for a zero
	// interval; the loop stops at a non-finite tick or one that rounding
	// keeps where it was, and past its 3000-tick safe limit there are none
	if interval == 0 || math.IsNaN(interval) || math.IsInf(interval, 0) {
		return s
	}
	lo := roundTo(math.Ceil(ext[0]/interval)*interval, prec)
	hi := roundTo(math.Floor(ext[1]/interval)*interval, prec)
	if ext[0] < lo {
		s.ticks = append(s.ticks, ext[0])
	}
	for t := lo; t <= hi && !math.IsInf(t, 0) && !math.IsNaN(t) && !math.IsInf(hi, 0); {
		s.ticks = append(s.ticks, t)
		next := roundTo(t+interval, prec)
		if next == t {
			break
		}
		if len(s.ticks) > tickSafeLimit {
			s.ticks = nil
			return s
		}
		t = next
	}
	if n := len(s.ticks); n > 0 && ext[1] > s.ticks[n-1] {
		s.ticks = append(s.ticks, ext[1])
	}
	return s
}

// tickSafeLimit is Interval.getTicks' safeLimit.
const tickSafeLimit = 3000
