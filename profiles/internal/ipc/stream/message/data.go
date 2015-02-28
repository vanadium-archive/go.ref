package message

import (
	"fmt"

	"v.io/x/ref/profiles/internal/ipc/stream/id"
	"v.io/x/ref/profiles/internal/lib/iobuf"
)

// Data encapsulates an application data message.
type Data struct {
	VCI     id.VC // Must be non-zero.
	Flow    id.Flow
	flags   uint8
	Payload *iobuf.Slice
}

// Close returns true if the sender of the data message requested that the flow be closed.
func (d *Data) Close() bool { return d.flags&0x1 == 1 }

// SetClose sets the Close flag of the message.
func (d *Data) SetClose() { d.flags |= 0x1 }

// Release releases the Payload
func (d *Data) Release() {
	if d.Payload != nil {
		d.Payload.Release()
		d.Payload = nil
	}
}

func (d *Data) PayloadSize() int {
	if d.Payload == nil {
		return 0
	}
	return d.Payload.Size()
}

func (d *Data) String() string {
	return fmt.Sprintf("VCI:%d Flow:%d Flags:%02x Payload:(%d bytes)", d.VCI, d.Flow, d.flags, d.PayloadSize())
}
