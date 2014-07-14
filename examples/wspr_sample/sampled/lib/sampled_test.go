package lib

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"veyron2/context"
	"veyron2/ipc"
	"veyron2/naming"
	"veyron2/rt"
	"veyron2/vdl/vdlutil"
	"veyron2/verror"

	hps "veyron/examples/wspr_sample"
)

// getCacheClient initializes the runtime and creates a client binding.
func getCacheClient(address string) (hps.Cache, error) {
	rt.Init()

	// Bind to a rooted, terminal name to bypass the MountTable which isn't
	// actually used, nor needed, in these tests.
	s, err := hps.BindCache(naming.JoinAddressName(address, "//cache"))
	if err != nil {
		return nil, err
	}

	return s, nil
}

// TestValueSetGet tests setting values and then calling various Get* functions.
func TestValueSetGet(t *testing.T) {
	type testCase struct {
		mapFieldName    string
		nameOfGetMethod string
		v               interface{}
		shouldGetError  bool
	}
	tests := []testCase{
		testCase{"val", "Get", "Test", false},
		testCase{"val", "Get", 4, false},
		testCase{"val", "Get", struct {
			X int
			Y string
		}{4, "Test"}, false},
		testCase{"i32", "GetAsInt32", int32(1), false},
		testCase{"i64", "GetAsInt64", int64(1), false},
		testCase{"byt", "GetAsByte", byte(1), false},
		testCase{"ui32", "GetAsUint32", uint32(1), false},
		testCase{"ui64", "GetAsUint64", uint64(1), false},
		testCase{"fl32", "GetAsFloat32", float32(1), false},
		testCase{"fl64", "GetAsFloat64", float64(1), false},
		testCase{"b", "GetAsBool", true, false},
		testCase{"s", "GetAsString", "Test", false},
		testCase{"err", "GetAsError", verror.ToStandard(errors.New("Test Error")), false},
		testCase{"err_i8", "GetAsByte", int64(4), true},
		testCase{"err_i64", "GetAsInt64", int8(1), true},
		testCase{"err_string", "GetAsString", true, true},
	}
	r := rt.Init()
	ctx := r.NewContext()

	s, endpoint, err := StartServer(r)
	if err != nil {
		t.Fatal("failed to start server: ", err)
	}
	defer s.Stop()
	c, err := getCacheClient(endpoint.String())
	if err != nil {
		t.Fatal("failed to connect client: ", err)
	}
	for _, test := range tests {
		// Call Set().
		if err := c.Set(ctx, test.mapFieldName, test.v); err != nil {
			t.Errorf("error setting: %v (test case: %v)", err, test)
			continue
		}

		meth := reflect.ValueOf(c).MethodByName(test.nameOfGetMethod)
		out := meth.Call([]reflect.Value{reflect.ValueOf(ctx), reflect.ValueOf(test.mapFieldName)})
		if !test.shouldGetError {
			if out[1].Interface() != nil {
				t.Errorf("error getting: %v (test case: %v)", err, test)
				continue
			}
			if out[0].Interface() != test.v {
				t.Errorf("returned result does not match")
			}
		} else if out[1].Interface() == nil {
			t.Errorf("expected error in case %v", test)
		}

	}
}

// settable mirrors the cache's Set method to provide a consistent way to populate test cases.
type settable interface {
	Set(ctx context.T, key string, val vdlutil.Any, opts ...ipc.CallOpt) error
}

// populateObject populates a settable with 12 values.
func populateObject(ctx context.T, s settable) error {
	if err := s.Set(ctx, "A", int8(3)); err != nil {
		return err
	}
	// Set "A" again to ensure it takes the second value.
	if err := s.Set(ctx, "A", "A"); err != nil {
		return err
	}
	if err := s.Set(ctx, "B", uint16(5)); err != nil {
		return err
	}
	if err := s.Set(ctx, "C", uint32(7)); err != nil {
		return err
	}
	if err := s.Set(ctx, "D", verror.ToStandard(errors.New("Err"))); err != nil {
		return err
	}
	if err := s.Set(ctx, "E", true); err != nil {
		return err
	}
	if err := s.Set(ctx, "F", float32(5.4)); err != nil {
		return err
	}
	if err := s.Set(ctx, "G", struct {
		X int
		Y string
	}{4, "G"}); err != nil {
		return err
	}
	if err := s.Set(ctx, "H", uint64(8)); err != nil {
		return err
	}
	if err := s.Set(ctx, "I", "I"); err != nil {
		return err
	}
	if err := s.Set(ctx, "J", float64(8.3)); err != nil {
		return err
	}
	if err := s.Set(ctx, "K", int64(2)); err != nil {
		return err
	}
	if err := s.Set(ctx, "L", int8(9)); err != nil {
		return err
	}
	return nil
}

// setupManyResults starts a server and client and populates the server with the values in populateObject.
func setupManyResults(t *testing.T) (hps.Cache, ipc.Server) {
	r := rt.Init()
	s, endpoint, err := StartServer(r)
	if err != nil {
		t.Fatal("failed to start server: ", err)
	}
	c, err := getCacheClient(endpoint.String())
	if err != nil {
		t.Fatal("failed to connect client: ", err)
	}

	if err := populateObject(r.NewContext(), c.(settable)); err != nil {
		t.Fatal("error populating cache: ", err)
	}

	return c, s
}

// settableMap is a map that implements the settable interface.
type settableMap map[string]vdlutil.Any

func (sm settableMap) Set(ctx context.T, key string, val vdlutil.Any, opts ...ipc.CallOpt) error {
	sm[key] = val
	return nil
}

// TestAsMap tests that AsMap returns the correct results.
func TestAsMap(t *testing.T) {
	c, s := setupManyResults(t)
	defer s.Stop()

	ctx := rt.R().NewContext()

	res, err := c.AsMap(ctx)
	if err != nil {
		t.Fatal("error calling AsMap: ", err)
	}

	m := settableMap(make(map[string]vdlutil.Any))
	if err := populateObject(ctx, m); err != nil {
		t.Fatal("error populating map: ", err)
	}

	for key, val := range m {
		otherVal := res[key]
		if val != otherVal {
			t.Errorf("didn't match: %v and %v", val, otherVal)
		}
	}
}

// TestKeyValuePairs tests that KeyValuePairs returns the correct results.
func TestKeyValuePairs(t *testing.T) {
	c, s := setupManyResults(t)
	defer s.Stop()

	ctx := rt.R().NewContext()
	res, err := c.KeyValuePairs(ctx)
	if err != nil {
		t.Fatal("error calling KeyValuePairs: ", err)
	}

	m := settableMap(make(map[string]vdlutil.Any))
	if err := populateObject(ctx, m); err != nil {
		t.Fatal("error populating map: ", err)
	}

	for _, kvp := range res {
		otherVal := m[kvp.Key]
		if kvp.Value != otherVal {
			t.Errorf("didn't match: %v and %v", kvp.Value, otherVal)
		}
	}
}

// TestKeyPageAndSize tests the KeyPage and size methods.
func TestKeyPageAndSize(t *testing.T) {
	c, s := setupManyResults(t)
	defer s.Stop()

	ctx := rt.R().NewContext()
	sz, err := c.Size(ctx)
	if err != nil {
		t.Fatal("error calling Size: ", err)
	}
	if sz != 12 {
		t.Fatal("wrong number of results: ", sz)
	}

	res, err := c.KeyPage(ctx, 1)
	if err != nil {
		t.Fatal("error calling AsMap: ", err)
	}

	if res[0] != "K" || res[1] != "L" || res[2] != "" {
		t.Fatalf("incorrect page results: %v", res)
	}
}

// TestMostRecentSet tests the MostRecentSet method.
func TestMostRecentSet(t *testing.T) {
	c, s := setupManyResults(t)
	defer s.Stop()

	ctx := rt.R().NewContext()

	timeBefore := time.Now().Unix()
	if err := c.Set(ctx, "B", int32(8)); err != nil {
		t.Fatal("error calling Set: ", err)
	}
	timeAfter := time.Now().Unix()

	kvp, setTime, err := c.MostRecentSet(ctx)
	if err != nil {
		t.Fatal("error calling MostRecentSet: ", err)
	}

	if kvp.Key != "B" || kvp.Value != int32(8) {
		t.Errorf("unexpected key value pair: %v", kvp)
	}

	if setTime < timeBefore || setTime > timeAfter {
		t.Errorf("time %v out of range [%v, %v]", setTime, timeBefore, timeAfter)
	}
}

// TestMultiGet tests the MultiGet method.
func TestMultiGet(t *testing.T) {
	c, s := setupManyResults(t)
	defer s.Stop()

	stream, err := c.MultiGet(rt.R().NewContext())
	if err != nil {
		t.Fatal("error calling MultiGet: ", err)
	}
	stream.Send("A")
	stream.Send("C")
	stream.Send("E")

	if item, err := stream.Recv(); err == nil {
		if item != "A" {
			t.Errorf("value for 'A' didn't match")
		}
	} else {
		t.Fatal("error on recv: %v", err)
	}

	if item, err := stream.Recv(); err == nil {
		if item != uint32(7) {
			t.Errorf("value for 'C' didn't match")
		}
	} else {
		t.Fatal("error on recv: %v", err)
	}

	if item, err := stream.Recv(); err == nil {
		if item != true {
			t.Errorf("value for 'E' didn't match")
		}
	} else {
		t.Fatal("error on recv: %v", err)
	}

	stream.CloseSend()
}
