package unit

import (
	"math"
	"testing"
)

func floatEq(first, second float64) bool {
	const maxPrecisionError = 0.00000001
	return math.Abs(float64(first)-float64(second)) <= maxPrecisionError
}

func TestDistanceAccessorsMetric(t *testing.T) {
	d := 1 * Kilometer
	if !floatEq(d.Kilometers(), 1) {
		t.Errorf("kilometers error: have %v, want %v", d.Kilometers(), 1)
	}
	if !floatEq(d.Meters(), 1000) {
		t.Errorf("meters error: have %v, want %v", d.Meters(), 1000)
	}
	if !floatEq(d.Centimeters(), 100000) {
		t.Errorf("centimeters error: have %v, want %v", d.Centimeters(), 100000)
	}
	if !floatEq(d.Millimeters(), 1000000) {
		t.Errorf("millimeters error: have %v, want %v", d.Millimeters(), 1000000)
	}
}

func TestDistanceAccessorsImperial(t *testing.T) {
	d := 1 * Mile
	if !floatEq(d.Miles(), 1) {
		t.Errorf("miles error: have %v, want %v", d.Miles(), 1)
	}
	if !floatEq(d.Yards(), 1760) {
		t.Errorf("yards error: have %v, want %v", d.Yards(), float64(5280)/3)
	}
	if !floatEq(d.Feet(), 5280) {
		t.Errorf("feet error: have %v, want %v", d.Feet(), 5280)
	}
	if !floatEq(d.Inches(), 63360) {
		t.Errorf("inches error: have %v, want %v", d.Inches(), 63360)
	}
}

func TestDistanceUnitConversions(t *testing.T) {
	d := 100.9 * Meter
	if !floatEq(d.Miles(), d.Feet()/5280) {
		t.Error("wrong miles <-> feet conversion")
	}
	if !floatEq(d.Kilometers(), d.Meters()/1000) {
		t.Error("wrong kilometers <-> meters conversion")
	}
	if !floatEq(d.Meters(), d.Centimeters()/100) {
		t.Error("wrong meters <-> centimeters conversion")
	}
	if !floatEq(d.Feet(), d.Meters()/0.3048) {
		t.Error("wrong feet <-> meters conversion")
	}
	if !floatEq(d.Feet(), d.Inches()/12) {
		t.Error("wrong feet -> inches conversion.")
	}
	if !floatEq(d.Centimeters(), d.Millimeters()/10) {
		t.Error("wrong centimeters -> millimeters conversion")
	}
}

func TestDistanceParseAgainstConstants(t *testing.T) {
	testcases := []struct {
		string   string
		expected Distance
	}{
		{"1mi", 1 * Mile},
		{"1km", 1 * Kilometer},
		{"1m", 1 * Meter},
		{"1yd", 1 * Yard},
		{"1ft", 1 * Foot},
		{"1in", 1 * Inch},
		{"1cm", 1 * Centimeter},
		{"1mm", 1 * Millimeter},
		{"0.1mi", 0.1 * Mile},
		{"0.1km", 0.1 * Kilometer},
		{"0.1m", 0.1 * Meter},
		{"0.1yd", 0.1 * Yard},
		{"0.1ft", 0.1 * Foot},
		{"0.1in", 0.1 * Inch},
		{"0.1cm", 0.1 * Centimeter},
		{"0.1mm", 0.1 * Millimeter},
		{"-18.2mi", -18.2 * Mile},
		{"-18.2km", -18.2 * Kilometer},
		{"-18.2m", -18.2 * Meter},
		{"-18.2yd", -18.2 * Yard},
		{"-18.2ft", -18.2 * Foot},
		{"-18.2in", -18.2 * Inch},
		{"-18.2cm", -18.2 * Centimeter},
		{"-18.2mm", -18.2 * Millimeter},
		{"10mi9m", 10*Mile + 9*Meter},
		{"10km9m8cm", 10*Kilometer + 9*Meter + 8*Centimeter},
		{"10m9m8m", 27 * Meter},
		{"-1cm100cm20cm2m", -1 * (2*Meter + 121*Centimeter)},
	}
	for _, tc := range testcases {
		d, err := ParseDistance(tc.string)
		if err != nil {
			t.Errorf("ParseDistance(%q) returned error: %v", tc.string, err)
			continue
		}
		if !floatEq(d.Millimeters(), tc.expected.Millimeters()) {
			t.Errorf("ParseDistance(%q).Millimeters() = %v; want %v", tc.string, d.Millimeters(), tc.expected.Millimeters())
		}
	}
}

func TestDistanceParseAgainstParse(t *testing.T) {
	testcases := []struct {
		first, second string
	}{
		// Singles.
		{"1mi", "5280ft"},
		{"1km", "1000m"},
		{"1m", "100cm"},
		{"1yd", "3ft"},
		{"1ft", "12in"},
		{"1in", "2.54cm"},
		{"1cm", "10mm"},
		{"-1cm", "-10mm"},
		{"10mi", "52800ft"},
		{"10km", "10000m"},
		{"10m", "1000cm"},
		{"10yd", "30ft"},
		{"10ft", "120in"},
		{"10in", "25.4cm"},
		{"10cm", "100mm"},
		{"-10cm", "-100mm"},
		{"0.1mi", "528ft"},
		{"0.1km", "100m"},
		{"0.1m", "10cm"},
		{"0.1yd", "0.3ft"},
		{"0.1ft", "1.2in"},
		{"0.1in", "0.254cm"},
		{"0.1cm", "1mm"},
		{"-0.1cm", "-1mm"},
		// Pairs.
		{"1mi1km", "1mi1000m"},
		{"1mi1m", "1mi100cm"},
		{"1mi1yd", "1mi3ft"},
		{"1mi1ft", "5281ft"},
		{"1mi1in", "5280ft1in"},
		{"1mi1cm", "5280ft10mm"},
		{"1mi1mm", "5280ft1mm"},
		{"-1mi1mm", "-5280ft1mm"},
		{"1km1m", "1001m"},
		{"1km1yd", "1000m3ft"},
		{"1km1ft", "1000m12in"},
		{"1km1in", "1000m2.54cm"},
		{"1km1cm", "1000m10mm"},
		{"1km1mm", "1000m0.1cm"},
		{"-1km1mm", "-1000m0.1cm"},
		{"1m1yd", "3ft100cm"},
		{"1m1ft", "12in100cm"},
		{"1m1in", "102.54cm"},
		{"1m1cm", "101cm"},
		{"1m1mm", "1001mm"},
		{"-1m1mm", "-1001mm"},
		{"1yd1ft", "4ft"},
		{"1yd1in", "3ft1in"},
		{"1yd1cm", "3ft10mm"},
		{"1yd1mm", "3ft0.1cm"},
		{"-1yd1mm", "-3ft0.1cm"},
		{"1ft1in", "13in"},
		{"1ft1cm", "12in10mm"},
		{"1ft1mm", "12in0.1cm"},
		{"-1ft1mm", "-12in0.1cm"},
		{"1in1cm", "1in10mm"},
		{"1in1mm", "1in0.1cm"},
		{"-1in1mm", "-1in0.1cm"},
		{"1cm1mm", "11mm"},
		// Complex.
		{"1mi1km1m1yd1ft1in1cm1mm", "1001m5284ft1in11mm"},
		{"-1mi1km1m1yd1ft1in1cm1mm", "-1001m5284ft1in11mm"},
		// CAPS.
		{"1Mi", "1mI"},
		{"1mi", "1MI"},
		{"1Km", "1kM"},
		{"1km", "1KM"},
		{"1M", "1m"},
		{"1Yd", "1yD"},
		{"1yd", "1YD"},
		{"1Ft", "1fT"},
		{"1ft", "1FT"},
		{"1In", "1iN"},
		{"1in", "1IN"},
		{"1Cm", "1cM"},
		{"1cm", "1CM"},
		{"1Mm", "1mM"},
		{"1mm", "1MM"},
	}
	for _, tc := range testcases {
		first, err := ParseDistance(tc.first)
		if err != nil {
			t.Errorf("ParseDistance(%q) returned error: %v", tc.first, err)
			continue
		}
		second, err := ParseDistance(tc.second)
		if err != nil {
			t.Errorf("ParseDistance(%q) returned error: err", tc.second, err)
			continue
		}
		if !floatEq(first.Millimeters(), second.Millimeters()) {
			t.Errorf("ParseDistance(%q).Millimeters() = %v; want %v = ParseDistance(%q).Millimeters()", tc.first, first.Millimeters(), second.Millimeters(), tc.second)
		}
	}
}

func TestDistanceString(t *testing.T) {
	testcases := []struct {
		d        Distance
		expected string
	}{
		{1 * Kilometer, "1km0m0mm"},
		{1 * Meter, "1m0mm"},
		{1 * Millimeter, "1mm"},
		{1200 * Meter, "1km200m0mm"},
		{1.3 * Kilometer, "1km300m0mm"},
		{-2.7 * Kilometer, "-2km700m0mm"},
		{5.33 * Millimeter, "5.33mm"},
		{3.0000000000001 * Millimeter, "3.0000000000001mm"},
		{9*Kilometer + 12*Meter, "9km12m0mm"},
		{9*Kilometer - 12*Meter, "8km988m0mm"},
		{12*Meter - 9*Kilometer, "-8km988m0mm"},
		{9*Kilometer + 2222*Meter + 1024.5*Millimeter, "11km223m24.5mm"},
		{-1*Kilometer - 3200.3*Meter - 22*Millimeter, "-4km200m322mm"},
	}
	for _, tc := range testcases {
		if tc.d.String() != tc.expected {
			t.Errorf("Got string %q; want %q", tc.d, tc.expected)
		}
	}
}

func TestDistanceParseError(t *testing.T) {
	testcases := []string{
		"1kf",
		"1fm",
		"1kmm",
		"m",
		"cm",
		"1ft 1mi",
		" 1ft 1mi",
		"1ft 1mi ",
		"1ft   1mi",
		"1mi-4km",
		"-3km-4m",
	}
	for _, ds := range testcases {
		if d, err := ParseDistance(ds); err == nil {
			t.Errorf("ParseDistance(%q) = %q; want error", ds, d)
		}
	}
}
