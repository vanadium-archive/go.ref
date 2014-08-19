package impl

// The config invoker is responsible for answering calls to the config service
// run as part of the node manager.  The config invoker converts RPCs to
// messages on channels that are used to listen on callbacks coming from child
// application instances.

import (
	"strconv"
	"sync"

	"veyron2/ipc"
)

type callbackState struct {
	sync.Mutex
	// channels maps callback identifiers and config keys to channels that
	// are used to communicate corresponding config values from child
	// processes.
	channels map[string]map[string]chan string
	// nextCallbackID provides the next callback identifier to use as a key
	// for the channels map.
	nextCallbackID int64
}

func newCallbackState() *callbackState {
	return &callbackState{
		channels: make(map[string]map[string]chan string),
	}
}

func (c *callbackState) generateID() string {
	c.Lock()
	defer c.Unlock()
	c.nextCallbackID++
	return strconv.FormatInt(c.nextCallbackID-1, 10)
}

func (c *callbackState) register(id, key string, channel chan string) {
	c.Lock()
	defer c.Unlock()
	if _, ok := c.channels[id]; !ok {
		c.channels[id] = make(map[string]chan string)
	}
	c.channels[id][key] = channel
}

func (c *callbackState) unregister(id string) {
	c.Lock()
	defer c.Unlock()
	delete(c.channels, id)
}

// configInvoker holds the state of a config service invocation.
type configInvoker struct {
	callback *callbackState
	// Suffix contains an identifier for the channel corresponding to the
	// request.
	suffix string
}

func (i *configInvoker) Set(_ ipc.ServerContext, key, value string) error {
	id := i.suffix
	i.callback.Lock()
	if _, ok := i.callback.channels[id]; !ok {
		i.callback.Unlock()
		return errInvalidSuffix
	}
	channel, ok := i.callback.channels[id][key]
	i.callback.Unlock()
	if !ok {
		return nil
	}
	channel <- value
	return nil
}
