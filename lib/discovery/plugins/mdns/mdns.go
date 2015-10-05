// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package mdns implements mDNS plugin for discovery service.
//
// In order to support discovery of a specific vanadium service, an instance
// is advertised in two ways - one as a vanadium service and the other as a
// subtype of vanadium service.
//
// For example, a vanadium printer service is advertised as
//
//    v23._tcp.local.
//    _<printer_service_uuid>._sub._v23._tcp.local.
package mdns

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"v.io/v23/context"
	"v.io/v23/discovery"

	ldiscovery "v.io/x/ref/lib/discovery"

	"github.com/pborman/uuid"
	mdns "github.com/presotto/go-mdns-sd"
)

const (
	v23ServiceName    = "v23"
	serviceNameSuffix = "._sub._" + v23ServiceName

	// Use short attribute names due to the txt record size limit.
	attrName       = "_n"
	attrInterface  = "_i"
	attrAddrs      = "_a"
	attrEncryption = "_e"

	// The prefix for attribute names for encoded large txt records.
	attrLargeTxtPrefix = "_x"

	// RFC 6763 limits each DNS txt record to 255 bytes and recommends to not have
	// the cumulative size be larger than 1300 bytes.
	//
	// TODO(jhahn): Figure out how to overcome this limit.
	maxTxtRecordLen       = 255
	maxTotalTxtRecordsLen = 1300
)

var (
	errMaxTxtRecordLenExceeded = errors.New("max txt record size exceeded")
)

type plugin struct {
	mdns      *mdns.MDNS
	adStopper *ldiscovery.Trigger

	subscriptionRefreshTime time.Duration
	subscriptionWaitTime    time.Duration
	subscriptionMu          sync.Mutex
	subscription            map[string]subscription // GUARDED_BY(subscriptionMu)
}

type subscription struct {
	count            int
	lastSubscription time.Time
}

func (p *plugin) Advertise(ctx *context.T, ad ldiscovery.Advertisement, done func()) (err error) {
	serviceName := ad.ServiceUuid.String() + serviceNameSuffix
	// We use the instance uuid as the host name so that we can get the instance uuid
	// from the lost service instance, which has no txt records at all.
	hostName := encodeInstanceUuid(ad.InstanceUuid)
	txt, err := createTxtRecords(&ad)
	if err != nil {
		done()
		return err
	}

	// Announce the service.
	err = p.mdns.AddService(serviceName, hostName, 0, txt...)
	if err != nil {
		done()
		return err
	}
	// Announce it as v23 service as well so that we can discover
	// all v23 services through mDNS.
	err = p.mdns.AddService(v23ServiceName, hostName, 0, txt...)
	if err != nil {
		done()
		return err
	}
	stop := func() {
		p.mdns.RemoveService(serviceName, hostName, 0, txt...)
		p.mdns.RemoveService(v23ServiceName, hostName, 0, txt...)
		done()
	}
	p.adStopper.Add(stop, ctx.Done())
	return nil
}

func (p *plugin) Scan(ctx *context.T, serviceUuid uuid.UUID, ch chan<- ldiscovery.Advertisement, done func()) error {
	var serviceName string
	if len(serviceUuid) == 0 {
		serviceName = v23ServiceName
	} else {
		serviceName = serviceUuid.String() + serviceNameSuffix
	}

	go func() {
		defer done()

		// Subscribe to the service if not subscribed yet or if we haven't refreshed in a while.
		p.subscriptionMu.Lock()
		sub := p.subscription[serviceName]
		sub.count++
		if time.Since(sub.lastSubscription) > p.subscriptionRefreshTime {
			p.mdns.SubscribeToService(serviceName)
			// Wait a bit to learn about neighborhood.
			time.Sleep(p.subscriptionWaitTime)
			sub.lastSubscription = time.Now()
		}
		p.subscription[serviceName] = sub
		p.subscriptionMu.Unlock()

		// Watch the membership changes.
		watcher, stopWatcher := p.mdns.ServiceMemberWatch(serviceName)
		defer func() {
			stopWatcher()
			p.subscriptionMu.Lock()
			sub := p.subscription[serviceName]
			sub.count--
			if sub.count == 0 {
				delete(p.subscription, serviceName)
				p.mdns.UnsubscribeFromService(serviceName)
			} else {
				p.subscription[serviceName] = sub
			}
			p.subscriptionMu.Unlock()
		}()

		for {
			var service mdns.ServiceInstance
			select {
			case service = <-watcher:
			case <-ctx.Done():
				return
			}
			ad, err := createAdvertisement(service)
			if err != nil {
				ctx.Error(err)
				continue
			}
			select {
			case ch <- ad:
			case <-ctx.Done():
				return
			}
		}
	}()
	return nil
}

func createTxtRecords(ad *ldiscovery.Advertisement) ([]string, error) {
	// Prepare a txt record with attributes and addresses to announce.
	txt := appendTxtRecord(nil, attrInterface, ad.InterfaceName)
	if len(ad.InstanceName) > 0 {
		txt = appendTxtRecord(txt, attrName, ad.InstanceName)
	}
	if len(ad.Addrs) > 0 {
		addrs := ldiscovery.PackAddresses(ad.Addrs)
		txt = appendTxtRecord(txt, attrAddrs, string(addrs))
	}
	if ad.EncryptionAlgorithm != ldiscovery.NoEncryption {
		enc := ldiscovery.PackEncryptionKeys(ad.EncryptionAlgorithm, ad.EncryptionKeys)
		txt = appendTxtRecord(txt, attrEncryption, string(enc))
	}
	for k, v := range ad.Attrs {
		txt = appendTxtRecord(txt, k, v)
	}
	txt, err := maybeSplitLargeTXT(txt)
	if err != nil {
		return nil, err
	}
	n := 0
	for _, v := range txt {
		n += len(v)
		if n > maxTotalTxtRecordsLen {
			return nil, errMaxTxtRecordLenExceeded
		}
	}
	return txt, nil
}

func appendTxtRecord(txt []string, k, v string) []string {
	var buf bytes.Buffer
	buf.WriteString(k)
	buf.WriteByte('=')
	buf.WriteString(v)
	kv := buf.String()
	txt = append(txt, kv)
	return txt
}

func createAdvertisement(service mdns.ServiceInstance) (ldiscovery.Advertisement, error) {
	// Note that service.Name starts with a host name, which is the instance uuid.
	p := strings.SplitN(service.Name, ".", 2)
	if len(p) < 1 {
		return ldiscovery.Advertisement{}, fmt.Errorf("invalid service name: %s", service.Name)
	}
	instanceUuid, err := decodeInstanceUuid(p[0])
	if err != nil {
		return ldiscovery.Advertisement{}, fmt.Errorf("invalid host name: %v", err)
	}

	ad := ldiscovery.Advertisement{Service: discovery.Service{InstanceUuid: instanceUuid}}
	if len(service.SrvRRs) == 0 && len(service.TxtRRs) == 0 {
		ad.Lost = true
		return ad, nil
	}

	ad.Attrs = make(discovery.Attributes)
	for _, rr := range service.TxtRRs {
		txt, err := maybeJoinLargeTXT(rr.Txt)
		if err != nil {
			return ldiscovery.Advertisement{}, err
		}

		for _, kv := range txt {
			p := strings.SplitN(kv, "=", 2)
			if len(p) != 2 {
				return ldiscovery.Advertisement{}, fmt.Errorf("invalid txt record: %s", txt)
			}
			switch k, v := p[0], p[1]; k {
			case attrName:
				ad.InstanceName = v
			case attrInterface:
				ad.InterfaceName = v
			case attrAddrs:
				if ad.Addrs, err = ldiscovery.UnpackAddresses([]byte(v)); err != nil {
					return ldiscovery.Advertisement{}, err
				}
			case attrEncryption:
				if ad.EncryptionAlgorithm, ad.EncryptionKeys, err = ldiscovery.UnpackEncryptionKeys([]byte(v)); err != nil {
					return ldiscovery.Advertisement{}, err
				}
			default:
				ad.Attrs[k] = v
			}
		}
	}
	return ad, nil
}

func New(host string) (ldiscovery.Plugin, error) {
	return newWithLoopback(host, false)
}

func newWithLoopback(host string, loopback bool) (ldiscovery.Plugin, error) {
	if len(host) == 0 {
		// go-mdns-sd doesn't answer when the host name is not set.
		// Assign a default one if not given.
		host = "v23()"
	}
	var v4addr, v6addr string
	if loopback {
		// To avoid interference from other mDNS server in unit tests.
		v4addr = "224.0.0.251:9999"
		v6addr = "[FF02::FB]:9999"
	}
	m, err := mdns.NewMDNS(host, v4addr, v6addr, loopback, false)
	if err != nil {
		// The name may not have been unique. Try one more time with a unique
		// name. NewMDNS will replace the "()" with "(hardware mac address)".
		if len(host) > 0 && !strings.HasSuffix(host, "()") {
			m, err = mdns.NewMDNS(host+"()", "", "", loopback, false)
		}
		if err != nil {
			return nil, err
		}
	}
	p := plugin{
		mdns:      m,
		adStopper: ldiscovery.NewTrigger(),
		// TODO(jhahn): Figure out a good subscription refresh time.
		subscriptionRefreshTime: 10 * time.Second,
		subscription:            make(map[string]subscription),
	}
	if loopback {
		p.subscriptionWaitTime = 5 * time.Millisecond
	} else {
		p.subscriptionWaitTime = 50 * time.Millisecond
	}
	return &p, nil
}
