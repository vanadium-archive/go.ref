// Package stream implements interfaces in veyron2/ipc/stream.
//
// It is split into multiple sub-packages in an attempt to keep the code
// healthier by limiting the dependencies between objects. Most users of this
// package however should only need to use the Runtime type defined in it
// (which provides a factory method to create stream.Manager objects).
//
// Package contents and dependencies are as follows:
//
//      * manager provides a factory for veyron2/ipc/stream.Manager objects.
//        It depends on the vif package.
//      * vif implements a VIF type that wraps over a net.Conn
//        and enables the creation of veyron2/ipc/stream.VC objects
//        over the underlying network connection.
//        It depends on the id, message and vc packages.
//      * message implements serialization and deserialization for
//        messages exchanged over a VIF.
//        It depends on the id package.
//      * vc provides types implementing veyron2/ipc/stream.VC and
//        veyron2/ipc/stream.Flow
//        It depends on the id and crypto packages.
//      * crypto provides types to secure communication over VCs.
//        It does not depend on any other package.
//      * id defines identifier types used by other packages.
//        It does not depend on any other package.
package stream

// A dump of some ideas/thoughts/TODOs arising from the first iteration of this
// package. Big ticket items like proxying and TLS/authentication are obvious
// and won't be missed. I just wanted to put some smaller items on record (in
// no particular order).
//
// (1) Garbage collection of VIFs: Create a policy to close the underlying
// network connection (and shutdown the VIF) when it is "inactive" (i.e., no VCs
// have existed on it for a while).
// (2) On the first write of a new flow, counters are stolen from a shared pool
// (to avoid a round trip of a "create flow" message followed by a "here are
// your counters" message). Currently, this happens on either end of the flow
// (on both the remote and local process). This doesn't need to be the case,
// the end that received the first message of the flow doesn't need to steal
// on its first write.
// (3) Should flow control counters be part of the Data message?
// If so, maybe the flowQ should have a lower priority than that of Data
// messages? At a higher level I'm thinking of ways to reduce the number
// of messages sent per flow. Currently, just creating a flow results in
// two messages - One where the initiator sends counters to the receiver
// and one where the receiver does the same. The first write does not
// block on receiving the counters because of the "steal from shared pool on
// first write" scheme, but still, sounds like too much traffic.
// (4) As an example of the above, consider the following code:
//     vc.Connect().Close()
// This will result in 3 messages. But ideally it should involve 0.
// (5) Encryption of control messages to protect from network sniffers.
