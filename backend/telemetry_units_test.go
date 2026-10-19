package main

import "testing"

func TestChartConvertFromSIIsIdentityWhenTheUnitIsTheSourceUnit(t *testing.T) {
	for _, u := range []string{"rad", "s", "m3", "J", "ratio", "A"} {
		conv, err := chartConvertFromSI(u, "x.y", u)
		if err != nil {
			t.Errorf("%s -> %s: %v", u, u, err)
			continue
		}
		if conv(3.5) != 3.5 {
			t.Errorf("%s -> %s is not an identity", u, u)
		}
	}
	// The source unit's display label is the same unit.
	conv, err := chartConvertFromSI("percent", "x.y", "%")
	if err != nil || conv(7) != 7 {
		t.Errorf("percent -> %%: %v", err)
	}
	// A declared unit with no display mapping still cannot become another unit.
	if _, err := chartConvertFromSI("rad", "x.y", "deg"); err == nil {
		t.Error("rad -> deg should still be an error")
	}
}
