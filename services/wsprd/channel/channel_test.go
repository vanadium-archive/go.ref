package channel_test

import (
	"fmt"
	"sync"
	"testing"

	"v.io/core/veyron2/vdl"
	"v.io/wspr/veyron/services/wsprd/channel"
)

func TestChannelRpcs(t *testing.T) {
	// Two channels are used and different test send in different directions.
	var bHandler channel.MessageSender
	channelA := channel.NewChannel(func(msg channel.Message) {
		bHandler(msg)
	})
	channelB := channel.NewChannel(channelA.HandleMessage)
	bHandler = channelB.HandleMessage

	type testCase struct {
		SendChannel *channel.Channel
		RecvChannel *channel.Channel
		Type        string
		ReqVal      int
		RespVal     int
		Err         error
	}

	// The list of tests to run concurrently in goroutines.
	// Half of the tests are with different type keys to test multiple type keys.
	// Half of the tests use the same type key
	// One test returns an error.
	tests := []testCase{}
	const reusedTypeName string = "reusedTypeName"
	expectedNumSuccessfulEachDirection := 128
	for i := 0; i < expectedNumSuccessfulEachDirection; i++ {
		tests = append(tests, testCase{channelA, channelB, fmt.Sprintf("Type%d", i), i, i + 1000, nil})
		tests = append(tests, testCase{channelB, channelA, reusedTypeName, -i - 1, -i - 1001, nil})
	}
	expectedNumFailures := 1
	tests = append(tests, testCase{channelB, channelA, "Type3", 0, 0, fmt.Errorf("TestError")})
	expectedNumCalls := expectedNumSuccessfulEachDirection*2 + expectedNumFailures
	callCountLock := sync.Mutex{}
	numCalls := 0

	// reusedHandler handles requests to the same type name.
	reusedHandler := func(v *vdl.Value) (*vdl.Value, error) {
		callCountLock.Lock()
		numCalls++
		callCountLock.Unlock()

		return vdl.Int64Value(v.Int() - 1000), nil
	}

	wg := sync.WaitGroup{}
	wg.Add(len(tests))
	var testGoRoutine = func(i int, test testCase) {
		defer wg.Done()

		// Get the message handler. Either the reused handle or a unique handle for this
		// test, depending on the type name.
		var handler func(v *vdl.Value) (*vdl.Value, error)
		if test.Type == reusedTypeName {
			handler = reusedHandler
		} else {
			handler = func(v *vdl.Value) (*vdl.Value, error) {
				callCountLock.Lock()
				numCalls++
				callCountLock.Unlock()

				if got, want := v, vdl.Int64Value(int64(test.ReqVal)); !vdl.EqualValue(got, want) {
					t.Errorf("For test %d, got %v, want %v", i, got, want)
				}
				return vdl.Int64Value(int64(test.RespVal)), test.Err
			}
		}
		test.RecvChannel.RegisterRequestHandler(test.Type, handler)

		// Perform the RPC.
		result, err := test.SendChannel.PerformRpc(test.Type, vdl.Int64Value(int64(test.ReqVal)))
		if test.Err != nil {
			if err == nil {
				t.Errorf("For test %d, expected an error but didn't get one", i)
			}
		} else {
			if err != nil {
				t.Errorf("For test %d, received unexpected error %v", i, err)
				return
			}
			if got, want := result, vdl.Int64Value(int64(test.RespVal)); !vdl.EqualValue(got, want) {
				t.Errorf("For test %d, got %v, want %v", i, got, want)
			}
		}
	}
	for i, test := range tests {
		go testGoRoutine(i, test)
	}

	wg.Wait()

	if numCalls != expectedNumCalls {
		t.Errorf("Expected to receive %d rpcs, but only got %d", expectedNumCalls, numCalls)
	}
}
