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
	"encoding/hex"
	"fmt"
	"strconv"
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

	// The attribute names should not exceed 4 bytes due to the txt record
	// size limit.
	attrServiceUuid = "_srv"
	attrInterface   = "_itf"
	attrAddr        = "_adr"
	// TODO(jhahn): Remove attrEncryptionAlgorithm.
	attrEncryptionAlgorithm = "_xxx"
	attrEncryptionKeys      = "_key"
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

func (p *plugin) Advertise(ctx *context.T, ad ldiscovery.Advertisement) error {
	serviceName := ad.ServiceUuid.String() + serviceNameSuffix
	// We use the instance uuid as the host name so that we can get the instance uuid
	// from the lost service instance, which has no txt records at all.
	hostName := hex.EncodeToString(ad.InstanceUuid)
	txt, err := createTXTRecords(&ad)
	if err != nil {
		return err
	}

	// Announce the service.
	err = p.mdns.AddService(serviceName, hostName, 0, txt...)
	if err != nil {
		return err
	}
	// Announce it as v23 service as well so that we can discover
	// all v23 services through mDNS.
	err = p.mdns.AddService(v23ServiceName, hostName, 0, txt...)
	if err != nil {
		return err
	}
	stop := func() {
		p.mdns.RemoveService(serviceName, hostName, 0, txt...)
		p.mdns.RemoveService(v23ServiceName, hostName, 0, txt...)
	}
	p.adStopper.Add(stop, ctx.Done())
	return nil
}

func (p *plugin) Scan(ctx *context.T, serviceUuid uuid.UUID, ch chan<- ldiscovery.Advertisement) error {
	var serviceName string
	if len(serviceUuid) == 0 {
		serviceName = v23ServiceName
	} else {
		serviceName = serviceUuid.String() + serviceNameSuffix
	}

	go func() {
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
			ad, err := decodeAdvertisement(service)
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

func createTXTRecords(ad *ldiscovery.Advertisement) ([]string, error) {
	// Prepare a TXT record with attributes and addresses to announce.
	//
	// TODO(jhahn): Currently, the packet size is limited to 2000 bytes in
	// go-mdns-sd package. Think about how to handle a large number of TXT
	// records.
	txt := make([]string, 0, len(ad.Attrs)+4)
	txt = append(txt, fmt.Sprintf("%s=%s", attrServiceUuid, ad.ServiceUuid))
	txt = append(txt, fmt.Sprintf("%s=%s", attrInterface, ad.InterfaceName))
	for k, v := range ad.Attrs {
		txt = append(txt, fmt.Sprintf("%s=%s", k, v))
	}
	for _, a := range ad.Addrs {
		txt = append(txt, fmt.Sprintf("%s=%s", attrAddr, a))
	}
	txt = append(txt, fmt.Sprintf("%s=%d", attrEncryptionAlgorithm, ad.EncryptionAlgorithm))
	for _, k := range ad.EncryptionKeys {
		txt = append(txt, fmt.Sprintf("%s=%s", attrEncryptionKeys, k))
	}
	return txt, nil
}

func decodeAdvertisement(service mdns.ServiceInstance) (ldiscovery.Advertisement, error) {
	// Note that service.Name starts with a host name, which is the instance uuid.
	p := strings.SplitN(service.Name, ".", 2)
	if len(p) < 1 {
		return ldiscovery.Advertisement{}, fmt.Errorf("invalid host name: %s", service.Name)
	}
	instanceUuid, err := hex.DecodeString(p[0])
	if err != nil {
		return ldiscovery.Advertisement{}, fmt.Errorf("invalid host name: %v", err)
	}

	ad := ldiscovery.Advertisement{
		Service: discovery.Service{
			InstanceUuid: instanceUuid,
			Attrs:        make(discovery.Attributes),
		},
		Lost: len(service.SrvRRs) == 0 && len(service.TxtRRs) == 0,
	}

	for _, rr := range service.TxtRRs {
		for _, txt := range rr.Txt {
			kv := strings.SplitN(txt, "=", 2)
			if len(kv) != 2 {
				return ldiscovery.Advertisement{}, fmt.Errorf("invalid txt record: %s", txt)
			}
			switch k, v := kv[0], kv[1]; k {
			case attrServiceUuid:
				ad.ServiceUuid = uuid.Parse(v)
			case attrInterface:
				ad.InterfaceName = v
			case attrAddr:
				ad.Addrs = append(ad.Addrs, v)
			case attrEncryptionAlgorithm:
				a, _ := strconv.Atoi(v)
				ad.EncryptionAlgorithm = ldiscovery.EncryptionAlgorithm(a)
			case attrEncryptionKeys:
				ad.EncryptionKeys = append(ad.EncryptionKeys, ldiscovery.EncryptionKey(v))
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
