package lib

import (
	"testing"
)

func TestWsNames(t *testing.T) {
	testdata := map[string]string{
		"/@3@tcp@127.0.0.1:46504@d7b41510a6e78033ed86e38efb61ef52@4@6@m@@":                     "/@3@ws@127.0.0.1:46504@d7b41510a6e78033ed86e38efb61ef52@4@6@m@@",
		"/@3@tcp4@example.com:46504@d7b41510a6e78033ed86e38efb61ef52@4@6@m@@/more/stuff":       "/@3@ws@example.com:46504@d7b41510a6e78033ed86e38efb61ef52@4@6@m@@/more/stuff",
		"/@3@ws@example.com:46504@d7b41510a6e78033ed86e38efb61ef52@4@6@m@@/more/stuff":         "/@3@ws@example.com:46504@d7b41510a6e78033ed86e38efb61ef52@4@6@m@@/more/stuff",
		"/@3@tcp@[::]:60624@21ba0c2508adfe8507eb953e526bd5a2@4@6@m@@":                          "/@3@ws@[::]:60624@21ba0c2508adfe8507eb953e526bd5a2@4@6@m@@",
		"/@3@tcp4@tcpexampletcp.com:46504@d7b41510a6e78033ed86e38efb61ef52@4@6@m@@/more/stuff": "/@3@ws@tcpexampletcp.com:46504@d7b41510a6e78033ed86e38efb61ef52@4@6@m@@/more/stuff",
		"/example.com:12345":                          "/@3@ws@example.com:12345@00000000000000000000000000000000@@@m@@",
		"/example.com:12345/more/stuff/in/suffix/tcp": "/@3@ws@example.com:12345@00000000000000000000000000000000@@@m@@/more/stuff/in/suffix/tcp",
	}

	for name, expectedWsName := range testdata {
		inputNames := []string{name}
		actualWsNames, err := EndpointsToWs(inputNames)
		if err != nil {
			t.Fatal(err)
		}
		expectedWsNames := []string{expectedWsName}
		if len(actualWsNames) != 1 || actualWsNames[0] != expectedWsNames[0] {
			t.Errorf("expected wsNames(%v) to be %v but got %v", inputNames, expectedWsNames, actualWsNames)
		}
	}
}
