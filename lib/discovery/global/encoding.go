// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package global

import (
	"strings"

	"v.io/v23/discovery"
	"v.io/v23/naming"
	"v.io/v23/vom"
)

// encodeAdToSuffix encodes the ad.Id and the ad.Attributes into the suffix at
// which we mount the advertisement.
//
// TODO(suharshs): Currently only the id and the attributes are encoded; we may
// want to encode the rest of the advertisement someday?
func encodeAdToSuffix(ad *discovery.Advertisement) (string, error) {
	b, err := vom.Encode(ad.Attributes)
	if err != nil {
		return "", err
	}
	// Escape suffixDelim to use it as our delimeter between the id and the attrs.
	id := ad.Id.String()
	attr := naming.EncodeAsNameElement(string(b))
	return naming.Join(id, attr), nil
}

// decodeAdFromSuffix decodes s into an advertisement.
func decodeAdFromSuffix(in string) (*discovery.Advertisement, error) {
	parts := strings.SplitN(in, "/", 2)
	if len(parts) != 2 {
		return nil, NewErrAdInvalidEncoding(nil, in)
	}
	id, attrs := parts[0], parts[1]
	attrs, ok := naming.DecodeFromNameElement(attrs)
	if !ok {
		return nil, NewErrAdInvalidEncoding(nil, in)
	}
	ad := &discovery.Advertisement{}
	var err error
	if ad.Id, err = discovery.ParseAdId(id); err != nil {
		return nil, err
	}
	if err = vom.Decode([]byte(attrs), &ad.Attributes); err != nil {
		return nil, err
	}
	return ad, nil
}
