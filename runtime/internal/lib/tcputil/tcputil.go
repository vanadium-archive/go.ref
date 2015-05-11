// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// package tcputil contains functions commonly used to manipulate TCP
// connections.
package tcputil

import (
	"net"
	"time"
)

const keepAlivePeriod = 30 * time.Second

// EnableTCPKeepAlive enabled the KeepAlive option on a TCP connection.
//
// Some cloud providers (like Google Compute Engine) blackhole inactive TCP
// connections, we need to set TCP keep alive option to prevent that.
// See: https://developers.google.com/compute/docs/troubleshooting#communicatewithinternet
//
// The same problem can happen when one end of a TCP connection dies and the
// TCP FIN or RST packet doesn't reach the other end, e.g. when the machine
// dies, falls off the network, or when there is packet loss. So, it is best to
// enable this option for all TCP connections.
func EnableTCPKeepAlive(conn net.Conn) error {
	if tcpconn, ok := conn.(*net.TCPConn); ok {
		if err := tcpconn.SetKeepAlivePeriod(keepAlivePeriod); err != nil {
			return err
		}
		return tcpconn.SetKeepAlive(true)
	}
	return nil
}
