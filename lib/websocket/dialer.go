// +build !nacl
package websocket

import (
	"github.com/gorilla/websocket"
	"net"
	"net/http"
	"net/url"
)

func Dial(address string) (net.Conn, error) {
	conn, err := net.Dial("tcp", address)
	if err != nil {
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

	return WebsocketConn(ws), nil
}
