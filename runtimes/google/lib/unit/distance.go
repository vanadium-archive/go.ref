package unit

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// metersInFoot denotes the number of meters in one foot, which is *exactly*
// 0.3048 (i.e., there is no rounding).
const (
	metersInFoot = 0.3048
	// MaxDistance represents the largest expressable distance.
	MaxDistance Distance = math.MaxFloat64
)

// Distance represents a physical distance with floating-point precision.
// It can be expressed in both metric and imperial units.
//
// Distance is internally stored in millimeter units.
type Distance float64

// Common distances.  To convert an integer number of units into a Distance,
// multiply:
//       fmt.Print(5*unit.Meter) // prints "5m0mm"
//
// To count a number of units in a Distance, divide:
//       d := 5*unit.Meter
//       fmt.Print(int64(d/unit.Centimeter)) // prints 500
//
// Distances can be added or subtracted:
//       fmt.Print(5*unit.Meter + 3*unit.Centimeter) // prints "5m30mm"
//       fmt.Print(3*unit.Kilometer - 2*unit.Kilometer) // prints "1km0m0mm"
const (
	Millimeter Distance = 1
	Centimeter          = 10 * Millimeter
	Meter               = 1000 * Millimeter
	Kilometer           = 1000 * Meter
	Foot                = metersInFoot * Meter
	Inch                = Foot / 12
	Yard                = 3 * Foot
	Mile                = 1760 * Yard
)

var parseRegexp = regexp.MustCompile("^([0-9\\+\\-\\.eE]*)([mM][iI]|[kK][mM]|[mM][mM]|[cC][mM]|[fF][tT]|[iI][nN]|[fF][tT]|[yY][dD]|[mM])")

// Parse parses the provided string and returns a new Distance.  The provided
// string must be of the following format:
//
//         [+-]*(<positive_float_num>("mi"|"km"|"m"|"yd"|"ft"|"in"|"cm"|"mm"))+
//
// For example, the following are legitimate distance strings:
//
//         "10.2mi8km0.3m12yd100ft18in12.82cm22mm"
//         "10km12m88cm"
//         "12yd8ft3in"
//         "-12yd3.2ft"
//
// ParseDistance will return an error if the string doesn't conform to the
// above format.
func ParseDistance(value string) (Distance, error) {
	var total Distance
	s := value

	// See if the string starts with a +- sign and if so consume it.
	neg := false
	if len(s) > 0 && s[0] == '+' {
		s = s[1:]
	} else if len(s) > 0 && s[0] == '-' {
		s = s[1:]
		neg = true
	}
	match := parseRegexp.FindStringSubmatch(s)
	if match == nil {
		return 0, fmt.Errorf("invalid distance string: %q", value)
	}
	for match != nil && len(match) == 3 {
		full, value, unit := match[0], match[1], match[2]
		// Parse value.
		f, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return 0, fmt.Errorf("error parsing float in distance string %q: %s", value, err)
		}
		if f < 0 {
			return 0, fmt.Errorf("can't use negative float unit count in distance string %q: %f", value, f)
		}
		// Parse unit.
		var mult Distance
		switch strings.ToLower(unit) {
		case "mi":
			mult = Mile
		case "km":
			mult = Kilometer
		case "m":
			mult = Meter
		case "yd":
			mult = Yard
		case "ft":
			mult = Foot
		case "in":
			mult = Inch
		case "cm":
			mult = Centimeter
		case "mm":
			mult = Millimeter
		default: // should never happen
			return 0, fmt.Errorf("illegal unit in distance string %q: %q", value, unit)
		}
		// Add the newly parsed sub-distance to the total.
		total += Distance(f) * mult
		// Advance to the next matched sub-distance.
		s = s[len(full):]
		match = parseRegexp.FindStringSubmatch(s)
	}

	if len(s) > 0 {
		return 0, fmt.Errorf("invalid distance string: %q", value)
	}

	if neg {
		total *= -1
	}
	return total, nil
}

func (d Distance) String() string {
	var sign string
	if d < 0 {
		sign = "-"
	}
	v := math.Abs(float64(d))
	kms := math.Floor(v / float64(Kilometer))
	v = v - kms*float64(Kilometer)
	ms := math.Floor(v / float64(Meter))
	v = v - ms*float64(Meter)
	mms := v
	if kms > 0 {
		return fmt.Sprintf("%s%gkm%gm%gmm", sign, kms, ms, mms)
	}
	if ms > 0 {
		return fmt.Sprintf("%s%gm%gmm", sign, ms, mms)
	}
	return fmt.Sprintf("%s%gmm", sign, mms)
}

// Miles returns a distance value in mile units.
func (d Distance) Miles() float64 {
	return float64(d / Mile)
}

// Kilometers returns a distance value in kilometer units.
func (d Distance) Kilometers() float64 {
	return float64(d / Kilometer)
}

// Meters returns a distance value in meter units.
func (d Distance) Meters() float64 {
	return float64(d / Meter)
}

// Yards returns a distance value in yard units.
func (d Distance) Yards() float64 {
	return float64(d / Yard)
}

// Feet returns a distance value in foot units.
func (d Distance) Feet() float64 {
	return float64(d / Foot)
}

// Inches returns a distance value in inch units.
func (d Distance) Inches() float64 {
	return float64(d / Inch)
}

// Centimeters returns a distance value in centimeter units.
func (d Distance) Centimeters() float64 {
	return float64(d / Centimeter)
}

// Millimeters returns a distance value in millimeter units.
func (d Distance) Millimeters() float64 {
	return float64(d)
}
