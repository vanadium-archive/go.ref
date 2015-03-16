// Package proxy implements a proxy for the stream layer.
//
// Each process in vanadium is uniquely identified by a routing id
// (naming.RoutingID). A proxy routes messages
// (veyron/profiles/internal/ipc/stream/message) it receives on a network connection
// (net.Conn) to the network connection on which the destination process
// (identified by the routing id) is listening.
//
// Processes behind a NAT can use the proxy to export their services outside
// the NAT.
// Sample usage:
//    var proxyEP naming.Endpoint  // Endpoint of the proxy server
//    var manager stream.Manager   // Manager used to create and listen for VCs and Flows.
//    ln, ep, err := manager.Listen(proxyEP.Network(), proxyEP.String())
//    // Now ln.Accept() will return Flows initiated by remote processes through the proxy.
//
// The proxy implemented in this package operates as follows:
// - When an OpenVC message is received at the proxy, the RoutingID(R)
//   of the source endpoint is associated with the net.Conn the message
//   was received on.
// - This association is used to route messages destined for R to the
//   corresponding net.Conn
// - Servers can "listen" on the proxy's address by establishing a VC to the
//   proxy. Once the VC is established, messages received at the proxy destined
//   for the RoutingID of the server are forwarded to the net.Conn between the
//   server and the proxy.
//
// For example, consider the following three processes:
// - Proxy(P) with routing id Rp
// - A server (S) wishing to listen on the proxy's address with routing id Rs
// - A client (C) wishing to connect to S through the proxy with routing id Rc.
//
// Here is a valid sequence of events that makes that possible:
// (1) S establishes a VC with P over a net.Conn c1
//     As a result, P knows that any messages intended for Rs should be
//     forwarded on c1
// (2) C connects to P over a net.Conn c2 and attempts to establish a VC with S
//     using an OpenVC message.
//     The source endpoint of this message contains the routing id Rc while the
//     destination endpoint contains the routing id Rs.
// (3) The proxy sees this message and:
//     (a) Forwards the message over c1 (since Rs is mapped to c1)
//     (b) Updates its routing table so that messages intended for Rc are forwarded over c2
// (4) Any messages from S intended for the client received on c1 are forwarded
//     by the proxy over c2.
package proxy
