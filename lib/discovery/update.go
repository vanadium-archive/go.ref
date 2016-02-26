// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package discovery

import (
	"fmt"

	"v.io/v23/context"
	"v.io/v23/discovery"
)

// update is an implementation of discovery.Update.
type update struct {
	ad       discovery.Advertisement
	hash     AdHash
	dirAddrs []string
	lost     bool
}

func (u *update) IsLost() bool          { return u.lost }
func (u *update) Id() discovery.AdId    { return u.ad.Id }
func (u *update) InterfaceName() string { return u.ad.InterfaceName }

func (u *update) Addresses() []string {
	addrs := make([]string, len(u.ad.Addresses))
	copy(addrs, u.ad.Addresses)
	return addrs
}

func (u *update) Attribute(name string) string { return u.ad.Attributes[name] }

func (u *update) Attachment(ctx *context.T, name string) <-chan discovery.DataOrError {
	// TODO(jhahn): Handle lazy fetching.
	var r discovery.DataOrError
	if data := u.ad.Attachments[name]; len(data) > 0 {
		r.Data = make([]byte, len(data))
		copy(r.Data, data)
	}
	ch := make(chan discovery.DataOrError, 1)
	ch <- r
	close(ch)
	return ch
}

func (u *update) Advertisement() discovery.Advertisement {
	ad := discovery.Advertisement{
		Id:            u.ad.Id,
		InterfaceName: u.ad.InterfaceName,
	}
	ad.Addresses = make([]string, len(u.ad.Addresses))
	copy(ad.Addresses, u.ad.Addresses)

	ad.Attributes = make(discovery.Attributes, len(u.ad.Attributes))
	for k, v := range u.ad.Attributes {
		ad.Attributes[k] = v
	}

	ad.Attachments = make(discovery.Attachments, len(u.ad.Attachments))
	for k, v := range u.ad.Attachments {
		data := make([]byte, len(v))
		copy(data, v)
		ad.Attachments[k] = data
	}
	return ad
}

func (u *update) String() string {
	return fmt.Sprintf("{%v %s %s %v %v}", u.lost, u.ad.Id, u.ad.InterfaceName, u.ad.Addresses, u.ad.Attributes)
}

// NewUpdate returns a new update with the given advertisement information.
func NewUpdate(adinfo *AdInfo) discovery.Update {
	return &update{
		ad:       adinfo.Ad,
		hash:     adinfo.Hash,
		dirAddrs: adinfo.DirAddrs,
		lost:     adinfo.Lost,
	}
}
