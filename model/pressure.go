package model

// Context pressure is how full a session's context is against the window at
// which Claude Code compacts it. It is told in five bands, so the screen can
// colour it without the colour carrying a number.
const (
	// BandCalm is below 40 % of the window.
	BandCalm = iota
	// BandFair is from 40 % up to 60 %.
	BandFair
	// BandWarm is from 60 % up to 75 %.
	BandWarm
	// BandHot is from 75 % up to 90 %.
	BandHot
	// BandCritical is 90 % and over.
	BandCritical
	// Bands is the number of bands.
	Bands
)

// bandFloors are the ratios at which a band starts: a ratio equal to a floor is
// in the band that starts there.
var bandFloors = [Bands]float64{0, 0.40, 0.60, 0.75, 0.90}

// PressureBand is the band of a ratio of context to window. A ratio below 0, or
// one that is not a number, is calm; one over 1 is critical.
func PressureBand(ratio float64) int {
	band := BandCalm
	for b := BandFair; b < Bands; b++ {
		if ratio >= bandFloors[b] { // false for NaN: NaN is calm
			band = b
		}
	}
	return band
}

// ContextRatio is context over window, 0 when there is no window.
func ContextRatio(context, window int64) float64 {
	if window <= 0 || context <= 0 {
		return 0
	}
	return float64(context) / float64(window)
}

// DefaultCompactWindow is the window used when neither the Claude Code settings
// nor the environment name one.
const DefaultCompactWindow int64 = 1_000_000
