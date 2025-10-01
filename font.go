package pdf

// FontMetrics returns the maximum height above the baseline and below the
// baseline at the specified font size.  A 0,0 return indicates an unknown font.
func FontMetrics(font string, fontSize float64) (habove, hbelow float64) {
	if m := metrics[font]; m == nil {
		return 0, 0
	} else {
		return float64(m.habove) * fontSize / 1000, float64(m.hbelow) * fontSize / 1000
	}
}

type fontMetrics struct {
	chars     [224]charMetrics // index is ASCII-32, max 223
	ligatures map[[2]byte]charMetrics
	kernpairs map[[2]byte]int16
	habove    int16
	hbelow    int16
}
type charMetrics [3]int16
