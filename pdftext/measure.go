package pdftext

// Measure returns the metrics of the specified string in the specified font at
// the specified size: specifically, the width, the height above the baseline,
// and the height below the baseline.  The string must not contain newlines.
// Characters that are not recognized (i.e., any non-ASCII characters) are given
// the size of an 'x', so the result could be imprecise if such characters are
// included.  The function returns zeros if the font is not known.
func Measure(s, font string, size float64) (width, habove, hbelow float64) {
	w, ha, hb := measure(s, font)
	return float64(w) * size / 1000.0, float64(ha) * size / 1000.0, float64(hb) * size / 1000.0
}
func measure(s, font string) (width, habove, hbelow int) {
	fm := metrics[font]
	if fm == nil {
		return 0, 0, 0
	}
	for s != "" {
		var cm [3]int16
		if len(s) > 1 {
			var key = [2]byte{s[0], s[1]}
			if fm.ligatures != nil {
				if cm = fm.ligatures[key]; cm[0] != 0 {
					s = s[2:]
				}
			}
			if fm.kernpairs != nil && cm[0] == 0 {
				width += int(fm.kernpairs[key])
			}
		}
		if cm[0] == 0 && s[0] >= 32 && s[0] <= 126 {
			cm = fm.chars[s[0]-32]
			s = s[1:]
		} else if cm[0] == 0 {
			cm = fm.chars['x'-32]
			s = s[1:]
		}
		width += int(cm[0])
		hbelow = min(hbelow, int(cm[1]))
		habove = max(habove, int(cm[2]))
	}
	hbelow = -hbelow
	return
}
