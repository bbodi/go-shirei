package perfcore

import (
	"math"
	"strconv"
	"strings"
)

// The overlay's numbers span from a 0.02ms GC pause to a 2-gigabyte heap, and
// they are read at a glance while something is going wrong. So every value
// carries its unit and steps up to the next one rather than growing digits:
// "2.0 MB", not "2036 KB", and "12,480", not "12480".

// FormatNumber is the fallback: three significant-ish digits, with thousands
// separators once the value gets big enough to need them.
func FormatNumber(v float64) string {
	switch a := math.Abs(v); {
	case a >= 1000:
		return group(strconv.FormatFloat(v, 'f', 0, 64))
	case a >= 100:
		return strconv.FormatFloat(v, 'f', 1, 64)
	case a >= 1:
		return strconv.FormatFloat(v, 'f', 1, 64)
	case a == 0:
		return "0"
	default:
		return strconv.FormatFloat(v, 'f', 2, 64)
	}
}

// FormatCount renders a whole number with thousands separators.
func FormatCount(v float64) string {
	return group(strconv.FormatFloat(v, 'f', 0, 64))
}

// FormatMs renders milliseconds. Sub-millisecond values keep two decimals,
// because that is the range a GC pause chart lives in.
func FormatMs(v float64) string {
	switch a := math.Abs(v); {
	case a >= 1000:
		return trim(v/1000) + " s"
	case a >= 1:
		return strconv.FormatFloat(v, 'f', 1, 64) + " ms"
	case a == 0:
		return "0 ms"
	default:
		return strconv.FormatFloat(v, 'f', 2, 64) + " ms"
	}
}

// FormatBytes renders a byte count.
func FormatBytes(v float64) string { return scale(v, 0) }

// FormatKB renders a value already expressed in kilobytes.
func FormatKB(v float64) string { return scale(v*1024, 1) }

// FormatMB renders a value already expressed in megabytes.
func FormatMB(v float64) string { return scale(v*1024*1024, 2) }

var units = []string{"B", "KB", "MB", "GB", "TB"}

// scale picks the unit that keeps the number under four digits, starting the
// search at from so a value that is already in KB never reads as bytes.
func scale(bytes float64, from int) string {
	v, i := math.Abs(bytes), 0
	for v >= 1024 && i < len(units)-1 {
		v /= 1024
		i++
	}
	if bytes < 0 {
		v = -v
	}
	// A zero never deserves a big unit, but it should still name the one the
	// caller was working in, so an empty row lines up with a full one.
	if bytes == 0 {
		i = from
	}
	return trim(v) + " " + units[i]
}

// trim renders one decimal place, dropping it when it is zero, so a column of
// values does not wobble between "2" and "2.0".
func trim(v float64) string {
	if math.Abs(v) >= 100 {
		return group(strconv.FormatFloat(v, 'f', 0, 64))
	}
	return strconv.FormatFloat(v, 'f', 1, 64)
}

// group inserts thousands separators into an already-rendered integer.
func group(s string) string {
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	if len(s) <= 3 {
		if neg {
			return "-" + s
		}
		return s
	}
	var b strings.Builder
	if neg {
		b.WriteByte('-')
	}
	lead := len(s) % 3
	if lead == 0 {
		lead = 3
	}
	b.WriteString(s[:lead])
	for i := lead; i < len(s); i += 3 {
		b.WriteByte(',')
		b.WriteString(s[i : i+3])
	}
	return b.String()
}
