package message

import (
	"testing"
	"testing/quick"

	"v.io/x/ref/profiles/internal/rpc/stream/id"
)

func TestCounterID(t *testing.T) {
	tests := []struct {
		vci id.VC
		fid id.Flow
	}{
		{0, 0},
		{1, 10},
		{0xffeeddcc, 0xffaabbcc},
	}
	for _, test := range tests {
		cid := MakeCounterID(test.vci, test.fid)
		if g, w := cid.VCI(), test.vci; g != w {
			t.Errorf("Got VCI %d want %d", g, w)
		}
		if g, w := cid.Flow(), test.fid; g != w {
			t.Errorf("Got Flow %d want %d", g, w)
		}
	}
}

func TestCounterID_Random(t *testing.T) {
	f := func(vci id.VC, fid id.Flow) bool {
		cid := MakeCounterID(vci, fid)
		return cid.VCI() == vci && cid.Flow() == fid
	}
	if err := quick.Check(f, nil); err != nil {
		t.Error(err)
	}
}

func TestCounters(t *testing.T) {
	f := func(vci id.VC, fid id.Flow, bytes []uint32) bool {
		c := NewCounters()
		var sum uint32
		for _, bin := range bytes {
			c.Add(vci, fid, bin)
			if len(c) != 1 {
				return false
			}
			sum += bin
			for cid, bout := range c {
				if cid.VCI() != vci || cid.Flow() != fid || bout != sum {
					return false
				}
			}
		}
		return true
	}
	if err := quick.Check(f, nil); err != nil {
		t.Error(err)
	}
}
