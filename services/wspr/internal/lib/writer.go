// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lib

type ResponseType int32

const (
	ResponseFinal                 ResponseType = 0
	ResponseStream                             = 1
	ResponseError                              = 2
	ResponseServerRequest                      = 3
	ResponseStreamClose                        = 4
	ResponseDispatcherLookup                   = 5
	ResponseAuthRequest                        = 6
	ResponseCancel                             = 7
	ResponseValidate                           = 8 // Request to validate caveats.
	ResponseLog                                = 9 // Sends a message to be logged.
	ResponseGranterRequest                     = 10
	ResponseBlessingsCacheMessage              = 11 // Update the blessings cache
	ResponseTypeMessage                        = 12
)

type Response struct {
	Type    ResponseType
	Message interface{}
}

// This is basically an io.Writer interface, that allows passing error message
// strings.  This is how the proxy will talk to the javascript/java clients.
type ClientWriter interface {
	Send(messageType ResponseType, data interface{}) error

	Error(err error)
}
