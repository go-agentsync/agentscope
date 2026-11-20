package model

import (
	"math"
	"testing"
)

func TestPressureBandBoundariesOnBothSides(t *testing.T) {
	cases := []struct {
		ratio float64
		want  int
	}{
		{0, BandCalm}, {0.399999, BandCalm},
		{0.40, BandFair}, {0.599999, BandFair},
		{0.60, BandWarm}, {0.749999, BandWarm},
		{0.75, BandHot}, {0.899999, BandHot},
		{0.90, BandCritical}, {1, BandCritical}, {7, BandCritical},
		{-1, BandCalm}, {math.NaN(), BandCalm},
	}
	for _, c := range cases {
		if got := PressureBand(c.ratio); got != c.want {
			t.Errorf("PressureBand(%v) = %d, want %d", c.ratio, got, c.want)
		}
	}
}

func TestContextRatio(t *testing.T) {
	if ContextRatio(150, 300) != 0.5 || ContextRatio(1, 0) != 0 || ContextRatio(-5, 10) != 0 || ContextRatio(0, 10) != 0 {
		t.Fatal("ContextRatio")
	}
}
