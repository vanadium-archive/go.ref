package channel_test

import (
	"fmt"
	"sync"
	"testing"
	"veyron.io/wspr/veyron/services/wsprd/channel"
)

func TestChannelRpcs(t *testing.T) {
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

	tests := []testCase{}

	expectedNumSuccessfulEachDirection := 128
	for i := 0; i < expectedNumSuccessfulEachDirection; i++ {
		tests = append(tests, testCase{channelA, channelB, fmt.Sprintf("Type%d", i), i, i + 1000, nil})
		tests = append(tests, testCase{channelB, channelA, "TypeB", -i, -i - 1000, nil})
	}

	expectedNumFailures := 1
	tests = append(tests, testCase{channelB, channelA, "Type3", 0, 0, fmt.Errorf("TestError")})

	expectedNumCalls := expectedNumSuccessfulEachDirection*2 + expectedNumFailures
	callCountLock := sync.Mutex{}
	numCalls := 0

	wg := sync.WaitGroup{}
	wg.Add(len(tests))
	for i, test := range tests {
		go func() {
			defer wg.Done()
			handler := func(v interface{}) (interface{}, error) {
				callCountLock.Lock()
				numCalls++
				callCountLock.Unlock()

				if test.ReqVal != v.(int) {
					t.Errorf("For test %d, expected request value was %q but got %q", i, test.ReqVal, v.(int))
				}
				return test.RespVal, test.Err
			}
			test.RecvChannel.RegisterRequestHandler(test.Type, handler)
			result, err := test.SendChannel.PerformRpc(test.Type, test.ReqVal)
			if test.Err != nil {
				if err == nil {
					t.Errorf("For test %d, expected an error but didn't get one", i)
				}
			} else {
				if err != nil {
					t.Errorf("For test %d, received unexpected error %v", i, err)
					return
				}
				if result.(int) != test.RespVal {
					t.Errorf("For test %d, expected response value was %q but got %q", i, test.RespVal, result.(int))
				}
			}
		}()
	}

	wg.Wait()

	if numCalls != expectedNumCalls {
		t.Errorf("Expected to receive %d rpcs, but only got %d", expectedNumCalls, numCalls)
	}
}
