// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build !nacl

package websocket

import (
	"net"
)

// Resolve performs a DNS resolution on the provided protocol and address.
func Resolve(protocol, address string) (string, string, error) {
	tcp := mapWebSocketToTCP[protocol]
	tcpAddr, err := net.ResolveTCPAddr(tcp, address)
	if err != nil {
		return "", "", err
	}
	return "ws", tcpAddr.String(), nil
}
