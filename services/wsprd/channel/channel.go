package channel

import (
	"fmt"
	"sync"
)

type RequestHandler func(interface{}) (interface{}, error)

type MessageSender func(Message)

type Channel struct {
	messageHandler MessageSender

	lastSeq          uint32
	handlers         map[string]RequestHandler
	pendingResponses map[uint32]chan Response
	lock             sync.Mutex
}

func NewChannel(messageHandler MessageSender) *Channel {
	return &Channel{
		messageHandler:   messageHandler,
		handlers:         map[string]RequestHandler{},
		pendingResponses: map[uint32]chan Response{},
	}
}

func (c *Channel) PerformRpc(typ string, body interface{}) (interface{}, error) {
	c.lock.Lock()
	c.lastSeq++
	lastSeq := c.lastSeq
	m := MessageRequest{Request{
		Type: typ,
		Seq:  lastSeq,
		Body: body,
	}}
	pending := make(chan Response, 1)
	c.pendingResponses[lastSeq] = pending
	c.lock.Unlock()

	go c.messageHandler(m)
	response := <-pending

	c.lock.Lock()
	delete(c.pendingResponses, lastSeq)
	c.lock.Unlock()

	if response.Err == "" {
		return response.Body, nil
	}
	return response.Body, fmt.Errorf(response.Err)
}

func (c *Channel) RegisterRequestHandler(typ string, handler RequestHandler) {
	c.lock.Lock()
	c.handlers[typ] = handler
	c.lock.Unlock()
}

func (c *Channel) handleRequest(req Request) {
	// Call handler.
	c.lock.Lock()
	handler, ok := c.handlers[req.Type]
	c.lock.Unlock()
	if !ok {
		panic(fmt.Errorf("Unknown handler: %s", req.Type))
	}

	result, err := handler(req.Body)
	errMsg := ""
	if err != nil {
		errMsg = err.Error()
	}
	m := MessageResponse{Response{
		ReqSeq: req.Seq,
		Err:    errMsg,
		Body:   result,
	}}
	c.messageHandler(m)
}

func (c *Channel) handleResponse(resp Response) {
	seq := resp.ReqSeq
	c.lock.Lock()
	pendingResponse, ok := c.pendingResponses[seq]
	c.lock.Unlock()
	if !ok {
		panic("Received invalid response code")
	}

	pendingResponse <- resp
}

func (c *Channel) HandleMessage(m Message) {
	switch r := m.(type) {
	case MessageRequest:
		c.handleRequest(r.Value)
	case MessageResponse:
		c.handleResponse(r.Value)
	default:
		panic(fmt.Sprintf("Unknown message type: %T", m))
	}
}
