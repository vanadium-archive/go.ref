package channel_nacl

import (
	"bytes"
	"fmt"
	"runtime/ppapi"

	"v.io/core/veyron2/vdl"
	"v.io/core/veyron2/vdl/valconv"
	"v.io/core/veyron2/vom"
	"v.io/wspr/veyron/services/wsprd/channel" // contains most of the logic, factored out for testing
)

type Channel struct {
	impl      *channel.Channel
	ppapiInst ppapi.Instance
}

type RequestHandler func(value *vdl.Value) (interface{}, error)

func sendMessageToBrowser(ppapiInst ppapi.Instance, m channel.Message) {
	var outBuf bytes.Buffer
	enc, err := vom.NewBinaryEncoder(&outBuf)
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

func (c *Channel) RegisterRequestHandler(typ string, handler RequestHandler) {
	wrappedHandler := func(val interface{}) (interface{}, error) {
		var v *vdl.Value
		if err := valconv.Convert(&v, val); err != nil {
			return nil, err
		}
		return handler(v)
	}
	c.impl.RegisterRequestHandler(typ, wrappedHandler)
}
func (c *Channel) PerformRpc(typ string, body interface{}) (*vdl.Value, error) {
	iface, err := c.impl.PerformRpc(typ, body)
	if err != nil {
		return nil, err
	}
	return iface.(*vdl.Value), nil
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
