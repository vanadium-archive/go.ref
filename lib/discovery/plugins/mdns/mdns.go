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

	idiscovery "v.io/x/ref/lib/discovery"

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
	adStopper *idiscovery.Trigger

	subscriptionRefreshTime time.Duration
	subscriptionWaitTime    time.Duration
	subscriptionMu          sync.Mutex
	subscription            map[string]subscription // GUARDED_BY(subscriptionMu)
}

type subscription struct {
	count            int
	lastSubscription time.Time
}

func (p *plugin) Advertise(ctx *context.T, ad idiscovery.Advertisement, done func()) (err error) {
	serviceName := uuid.UUID(ad.ServiceUuid).String() + serviceNameSuffix
	// We use the instance uuid as the host name so that we can get the instance uuid
	// from the lost service instance, which has no txt records at all.
	hostName := encodeInstanceUuid(ad.Service.InstanceUuid)
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

func (p *plugin) Scan(ctx *context.T, serviceUuid idiscovery.Uuid, ch chan<- idiscovery.Advertisement, done func()) error {
	var serviceName string
	if len(serviceUuid) == 0 {
		serviceName = v23ServiceName
	} else {
		serviceName = uuid.UUID(serviceUuid).String() + serviceNameSuffix
	}

	go func() {
		defer done()

		p.subscribeToService(serviceName)
		watcher, stopWatcher := p.mdns.ServiceMemberWatch(serviceName)
		defer func() {
			stopWatcher()
			p.unsubscribeFromService(serviceName)
		}()

		for {
			var service mdns.ServiceInstance
			select {
			case service = <-watcher:
			case <-time.After(p.subscriptionRefreshTime):
				p.refreshSubscription(serviceName)
				continue
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

func (p *plugin) subscribeToService(serviceName string) {
	p.subscriptionMu.Lock()
	sub := p.subscription[serviceName]
	sub.count++
	p.subscription[serviceName] = sub
	p.subscriptionMu.Unlock()
	p.refreshSubscription(serviceName)
}

func (p *plugin) unsubscribeFromService(serviceName string) {
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
}

func (p *plugin) refreshSubscription(serviceName string) {
	p.subscriptionMu.Lock()
	sub, ok := p.subscription[serviceName]
	if !ok {
		p.subscriptionMu.Unlock()
		return
	}
	// Subscribe to the service again if we haven't refreshed in a while.
	if time.Since(sub.lastSubscription) > p.subscriptionRefreshTime {
		p.mdns.SubscribeToService(serviceName)
		// Wait a bit to learn about neighborhood.
		time.Sleep(p.subscriptionWaitTime)
		sub.lastSubscription = time.Now()
	}
	p.subscription[serviceName] = sub
	p.subscriptionMu.Unlock()
}

func createTxtRecords(ad *idiscovery.Advertisement) ([]string, error) {
	// Prepare a txt record with attributes and addresses to announce.
	txt := appendTxtRecord(nil, attrInterface, ad.Service.InterfaceName)
	if len(ad.Service.InstanceName) > 0 {
		txt = appendTxtRecord(txt, attrName, ad.Service.InstanceName)
	}
	if len(ad.Service.Addrs) > 0 {
		addrs := idiscovery.PackAddresses(ad.Service.Addrs)
		txt = appendTxtRecord(txt, attrAddrs, string(addrs))
	}
	if ad.EncryptionAlgorithm != idiscovery.NoEncryption {
		enc := idiscovery.PackEncryptionKeys(ad.EncryptionAlgorithm, ad.EncryptionKeys)
		txt = appendTxtRecord(txt, attrEncryption, string(enc))
	}
	for k, v := range ad.Service.Attrs {
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

func createAdvertisement(service mdns.ServiceInstance) (idiscovery.Advertisement, error) {
	// Note that service.Name starts with a host name, which is the instance uuid.
	p := strings.SplitN(service.Name, ".", 2)
	if len(p) < 1 {
		return idiscovery.Advertisement{}, fmt.Errorf("invalid service name: %s", service.Name)
	}
	instanceUuid, err := decodeInstanceUuid(p[0])
	if err != nil {
		return idiscovery.Advertisement{}, fmt.Errorf("invalid host name: %v", err)
	}

	ad := idiscovery.Advertisement{Service: discovery.Service{InstanceUuid: instanceUuid}}
	if len(service.SrvRRs) == 0 && len(service.TxtRRs) == 0 {
		ad.Lost = true
		return ad, nil
	}

	ad.Service.Attrs = make(discovery.Attributes)
	for _, rr := range service.TxtRRs {
		txt, err := maybeJoinLargeTXT(rr.Txt)
		if err != nil {
			return idiscovery.Advertisement{}, err
		}

		for _, kv := range txt {
			p := strings.SplitN(kv, "=", 2)
			if len(p) != 2 {
				return idiscovery.Advertisement{}, fmt.Errorf("invalid txt record: %s", txt)
			}
			switch k, v := p[0], p[1]; k {
			case attrName:
				ad.Service.InstanceName = v
			case attrInterface:
				ad.Service.InterfaceName = v
			case attrAddrs:
				if ad.Service.Addrs, err = idiscovery.UnpackAddresses([]byte(v)); err != nil {
					return idiscovery.Advertisement{}, err
				}
			case attrEncryption:
				if ad.EncryptionAlgorithm, ad.EncryptionKeys, err = idiscovery.UnpackEncryptionKeys([]byte(v)); err != nil {
					return idiscovery.Advertisement{}, err
				}
			default:
				ad.Service.Attrs[k] = v
			}
		}
	}
	return ad, nil
}

func New(host string) (idiscovery.Plugin, error) {
	return newWithLoopback(host, 0, false)
}

func newWithLoopback(host string, port int, loopback bool) (idiscovery.Plugin, error) {
	if len(host) == 0 {
		// go-mdns-sd doesn't answer when the host name is not set.
		// Assign a default one if not given.
		host = "v23()"
	}
	var v4addr, v6addr string
	if port > 0 {
		v4addr = fmt.Sprintf("224.0.0.251:%d", port)
		v6addr = fmt.Sprintf("[FF02::FB]:%d", port)
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
		adStopper: idiscovery.NewTrigger(),
		// TODO(jhahn): Figure out a good subscription refresh time.
		subscriptionRefreshTime: 15 * time.Second,
		subscription:            make(map[string]subscription),
	}
	if loopback {
		p.subscriptionWaitTime = 5 * time.Millisecond
	} else {
		p.subscriptionWaitTime = 50 * time.Millisecond
	}
	return &p, nil
}
