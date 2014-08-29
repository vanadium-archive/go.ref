package syncgroup

// This file tests syncgroup/id.go

import (
	"testing"
)

// signum returns -1, 0, or 1 according to whether its argument is -ve, 0, or
// +ve, respectively.
func signum(x int) (result int) {
	if x < 0 {
		result = -1
	} else if x > 0 {
		result = 1
	}
	return result
}

// NewID is tested as a side-effect of the other routines.

// TestIsValid tests IsValid().
func TestIsValid(t *testing.T) {
	var zeroID ID
	var nonZeroID ID = NewID()

	if zeroID.IsValid() {
		t.Errorf("IsValid(%v) == true", zeroID)
	}
	if !nonZeroID.IsValid() {
		t.Errorf("IsValid(%v) == false", nonZeroID)
	}
}

// TestCompareIDs tests CompareIDs().
func TestCompareIDs(t *testing.T) {
	ids := []ID{ID{}, NewID(), NewID()}
	for i := 0; i != len(ids); i++ {
		for j := 0; j != len(ids); j++ {
			if (i == j) != (CompareIDs(ids[i], ids[j]) == 0) {
				t.Errorf("(CompareIDs(%v, %v) == 0) == %v, expected %v",
					ids[i], ids[j],
					CompareIDs(ids[i], ids[j]) == 0,
					i == j)
			}
			if signum(CompareIDs(ids[i], ids[j])) !=
				-signum(CompareIDs(ids[j], ids[i])) {
				t.Errorf("(signum(CompareIDs(%v, %v)) == %v != -(%v == signum(CompareIDs(%v, %v)))",
					ids[i], ids[j], signum(CompareIDs(ids[i], ids[j])),
					signum(CompareIDs(ids[j], ids[i])), ids[j], ids[i])
			}
		}
	}
}

// TestStringAndParseID tests String() and ParseID().
func TestStringAndParseID(t *testing.T) {
	var lenOfString int
	for i := 0; i != 10; i++ {
		var id ID
		var str string
		if i == 0 { // First time through, use the zero id, and remember the length.
			str = id.String()
			lenOfString = len(str)
			if lenOfString <= 0 {
				t.Errorf("len(id.String()) == %v <= 0", len(str))
			}
		} else { // Subsequently, use a new id.
			id = NewID()
			str = id.String()
		}
		if len(str) != lenOfString {
			t.Errorf("len(id.String()) == %v != %v", len(str), lenOfString)
		}
		var parsedID ID
		var err error
		parsedID, err = ParseID(str)
		if err != nil {
			t.Errorf("ParseID(%v) yields %v", str, err)
		}
		if CompareIDs(id, parsedID) != 0 {
			t.Errorf("ParseID(%v) == %v  !=  %v", str, parsedID, id)
		}
	}
}
