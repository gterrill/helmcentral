package main

import "testing"

// These cover relativeAngleDeg/relativeAngleLabel (assistant_geometry.go),
// the host-side arithmetic get_wind_forecast's optional course_deg uses
// (executeGetWindForecast, assistant_tools.go) so the model never has to
// work modular bearing subtraction out by eye - see ADR 0093 §2. Wind and
// wave directions are always reported as the direction they come FROM, so
// relativeAngleDeg(courseDeg, fromDeg) treats a result of 0 as dead ahead
// and 180 as dead astern.
func TestRelativeAngleDeg(t *testing.T) {
	cases := []struct {
		name      string
		courseDeg float64
		fromDeg   float64
		want      int
	}{
		// The motivating case: a 303°T course against a forecast SE (135°T)
		// wind is a following wind (168° off the bow), not the "port beam to
		// port quarter" the model once called it.
		{"following, course 303 wind 135", 303, 135, 168},
		// 303-45 normalises to 102 (wraps past 360, then folds back under
		// 180), not the raw 258.
		{"quarter, course 303 wind 45", 303, 45, 102},
		{"head, wraps across zero", 0, 350, 10},
		{"dead astern", 90, 270, 180},
		{"beam", 10, 100, 90},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := relativeAngleDeg(tc.courseDeg, tc.fromDeg); got != tc.want {
				t.Errorf("relativeAngleDeg(%g, %g) = %d, want %d", tc.courseDeg, tc.fromDeg, got, tc.want)
			}
		})
	}
}

// relativeBearingDeg is relativeAngleDeg's signed counterpart, added for the
// COLREGS encounter classifier (collision_colregs.go, ADR 0098): port and
// starboard are the same folded magnitude and only the signed 0-360 form
// keeps them apart.
func TestRelativeBearingDeg(t *testing.T) {
	cases := []struct {
		name       string
		headingDeg float64
		bearingDeg float64
		want       int
	}{
		{"dead ahead", 90, 90, 0},
		{"40 to starboard", 0, 40, 40},
		{"40 to port", 40, 0, 320},
		{"wraps past 360", 350, 10, 20},
		{"wraps past 0 the other way", 10, 350, 340},
		{"dead astern", 0, 180, 180},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := relativeBearingDeg(tc.headingDeg, tc.bearingDeg); got != tc.want {
				t.Errorf("relativeBearingDeg(%g, %g) = %d, want %d", tc.headingDeg, tc.bearingDeg, got, tc.want)
			}
		})
	}
}

func TestRelativeAngleLabel(t *testing.T) {
	cases := []struct {
		deg  int
		want string
	}{
		{0, "head"},
		{10, "head"},
		{45, "head"},
		{46, "bow"},
		{80, "bow"},
		{81, "beam"},
		{90, "beam"},
		{100, "beam"},
		{101, "quarter"},
		{102, "quarter"},
		{135, "quarter"},
		{136, "following"},
		{168, "following"},
		{180, "following"},
	}
	for _, tc := range cases {
		if got := relativeAngleLabel(tc.deg); got != tc.want {
			t.Errorf("relativeAngleLabel(%d) = %q, want %q", tc.deg, got, tc.want)
		}
	}
}
