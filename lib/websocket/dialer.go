// +build !nacl

package websocket

import (
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/gorilla/websocket"

	"v.io/x/ref/lib/tcputil"
)

func Dial(protocol, address string, timeout time.Duration) (net.Conn, error) {
	var then time.Time
	if timeout > 0 {
		then = time.Now().Add(timeout)
	}
	tcp := mapWebSocketToTCP[protocol]
	conn, err := net.DialTimeout(tcp, address, timeout)
	if err != nil {
		return nil, err
	}
	conn.SetReadDeadline(then)
	if err := tcputil.EnableTCPKeepAlive(conn); err != nil {
		return nil, err
	}
	u, err := url.Parse("ws://" + address)
	if err != nil {
		return nil, err
	}
	ws, _, err := websocket.NewClient(conn, u, http.Header{}, 4096, 4096)
	if err != nil {
		return nil, err
	}
	var zero time.Time
	conn.SetDeadline(zero)
	return WebsocketConn(ws), nil
}
