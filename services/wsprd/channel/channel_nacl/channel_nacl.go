package channel_nacl

import (
	"bytes"
	"fmt"
	"runtime/ppapi"

	"v.io/v23/vdl"
	"v.io/v23/vom"
	"v.io/wspr/veyron/services/wsprd/channel" // contains most of the logic, factored out for testing
)

type Channel struct {
	impl      *channel.Channel
	ppapiInst ppapi.Instance
}

func sendMessageToBrowser(ppapiInst ppapi.Instance, m channel.Message) {
	var outBuf bytes.Buffer
	enc, err := vom.NewEncoder(&outBuf)
	if err != nil {
		panic(fmt.Sprintf("Error beginning encoding: %v", err))
	}
	if err := enc.Encode(m); err != nil {
		panic(fmt.Sprintf("Error encoding message %v: %v", m, err))
	}
	outVar := ppapi.VarFromByteSlice(outBuf.Bytes())
	ppapiInst.PostMessage(outVar)
}

func NewChannel(ppapiInst ppapi.Instance) *Channel {
	sendMessageFunc := func(m channel.Message) {
		sendMessageToBrowser(ppapiInst, m)
	}
	return &Channel{
		impl:      channel.NewChannel(sendMessageFunc),
		ppapiInst: ppapiInst,
	}
}

func (c *Channel) RegisterRequestHandler(typ string, handler channel.RequestHandler) {
	c.impl.RegisterRequestHandler(typ, handler)
}

func (c *Channel) PerformRpc(typ string, body *vdl.Value) (*vdl.Value, error) {
	return c.impl.PerformRpc(typ, body)
}

func (c *Channel) HandleMessage(v ppapi.Var) {
	// Read input message
	b, err := v.AsByteSlice()
	if err != nil {
		panic(fmt.Sprintf("Cannot convert message to byte slice: %v", err))
	}

	buf := bytes.NewBuffer(b)
	dec, err := vom.NewDecoder(buf)
	if err != nil {
		panic(fmt.Sprintf("Error beginning decoding: %v", err))
	}

	var m channel.Message
	if err := dec.Decode(&m); err != nil {
		panic(fmt.Sprintf("Error decoding message: %v", err))
	}

	c.impl.HandleMessage(m)
}
