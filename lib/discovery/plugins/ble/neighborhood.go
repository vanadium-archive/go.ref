// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package ble implements a ble protocol to support the Vanadium discovery api.
// The package exports a discovery.Plugin that can be used to use ble for discovery.
// The advertising packet of Vanadium device should contain a manufacturer data field
// with the manufacturer id of 1001.  The first 8 bytes of the data is the hash of
// the services exported.  If the hash has not changed, then it is expected that the
// services and the properties they contain has not changed either.
package ble

import (
	"encoding/hex"
	"hash/fnv"
	"log"
	"reflect"
	"sync"
	"time"

	"github.com/paypal/gatt"
	"github.com/pborman/uuid"
	"v.io/x/ref/lib/discovery"
)

const (
	// TODO(bjornick): Make sure this is actually unique.
	manufacturerId = uint16(1001)
	ttl            = time.Minute * 5
)

var (
	// These constants are taken from:
	// https://developer.bluetooth.org/gatt/characteristics/Pages/CharacteristicsHome.aspx
	attrGapUuid  = gatt.UUID16(0x1800)
	attrGattUuid = gatt.UUID16(0x1801)
)

type bleCacheEntry struct {
	id             string
	advertisements map[string]*bleAdv
	hash           string
	lastSeen       time.Time
}

type bleNeighborhood struct {
	mu sync.Mutex

	// key is the hex encoded hash of the bleCacheEntry
	neighborsHashCache map[string]*bleCacheEntry
	// key is the hex encoded ID of the device.
	knownNeighbors map[string]*bleCacheEntry
	// key is the instance id
	services map[string]*gatt.Service
	// Scanners out standing calls to Scan that need be serviced.  Each time a
	// new device appears or disappears, the scanner is notified of the event.
	// The key is the unique scanner id for the scanner.
	scannersById map[int64]*scanner

	// scannersByService maps from human readable service uuid to a map of
	// scanner id to scanner.
	scannersByService map[string]map[int64]*scanner

	// If both sides try to connect to each other at the same time, then only
	// one will succeed and the other hangs forever.  This means that the side
	// that hangs won't ever start scanning or advertising again.  To avoid this
	// we timeout any connections that don't finish in under 4 seconds.  This
	// channel is closed when a connection has been made successfully, to notify
	// the cancel goroutine that it doesn't need to do anything.  This is a map
	// from the hex encoded device id to the channel to close.
	timeoutMap map[string]chan struct{}

	// The hash that we use to avoid multiple connections are stored in the
	// advertising data, so we need to store somewhere in the bleNeighorhood
	// until we are ready to save the new device data.  This map is
	// the keeper of the data.  This is a map from hex encoded device id to
	// hex encoded hash.
	pendingHashMap map[string]string
	name           string
	device         gatt.Device
	stopped        chan struct{}
	nextScanId     int64
}

func newBleNeighborhood(name string) (*bleNeighborhood, error) {
	b := &bleNeighborhood{
		neighborsHashCache: make(map[string]*bleCacheEntry),
		knownNeighbors:     make(map[string]*bleCacheEntry),
		name:               name,
		services:           make(map[string]*gatt.Service),
		scannersById:       make(map[int64]*scanner),
		scannersByService:  make(map[string]map[int64]*scanner),
		timeoutMap:         make(map[string]chan struct{}),
		pendingHashMap:     make(map[string]string),
		stopped:            make(chan struct{}),
	}
	if err := b.startBLEService(); err != nil {
		return nil, err
	}

	go b.checkTTL()
	return b, nil
}

func (b *bleNeighborhood) checkTTL() {
	for {
		select {
		case <-b.stopped:
			return
		case <-time.After(time.Minute):
			b.mu.Lock()
			now := time.Now()

			for k, entry := range b.neighborsHashCache {
				if entry.lastSeen.Add(ttl).Before(now) {
					delete(b.neighborsHashCache, k)
					delete(b.knownNeighbors, entry.id)
					for id, adv := range entry.advertisements {
						for _, scanner := range b.scannersById {
							scanner.handleLost(uuid.Parse(id), adv)
						}
					}
				}
			}
			b.mu.Unlock()
		}
	}
}

func (b *bleNeighborhood) addAdvertisement(adv bleAdv) {
	gattService := gatt.NewService(gatt.MustParseUUID(adv.serviceUuid.String()))
	for k, v := range adv.attrs {
		gattService.AddCharacteristic(gatt.MustParseUUID(k)).SetValue(v)
	}
	b.mu.Lock()
	b.services[string(adv.attrs[InstanceIdUuid])] = gattService
	v := make([]*gatt.Service, len(b.services))
	i := 0
	for _, s := range b.services {
		v[i] = s
		i++
	}
	b.mu.Unlock()
	b.device.SetServices(v)
}

func (b *bleNeighborhood) removeService(id string) {
	b.mu.Lock()
	delete(b.services, id)
	v := make([]*gatt.Service, 0, len(b.services))
	for _, s := range b.services {
		v = append(v, s)
	}
	b.mu.Unlock()
	b.device.SetServices(v)
}

func (b *bleNeighborhood) addScanner(uid discovery.Uuid) (chan *discovery.Advertisement, int64) {
	ch := make(chan *discovery.Advertisement)
	s := &scanner{
		uuid: uuid.UUID(uid),
		ch:   ch,
	}
	b.mu.Lock()
	id := b.nextScanId
	b.nextScanId++
	b.scannersById[id] = s
	key := uuid.UUID(uid).String()
	m, found := b.scannersByService[key]
	if !found {
		m = map[int64]*scanner{}
		b.scannersByService[key] = m
	}
	m[id] = s
	b.mu.Unlock()
	return ch, id
}

func (b *bleNeighborhood) removeScanner(id int64) {
	b.mu.Lock()
	scanner, found := b.scannersById[id]
	if found {
		scanner.stop()
	}
	delete(b.scannersById, id)
	key := scanner.uuid.String()
	delete(b.scannersByService[key], id)
	b.mu.Unlock()
}

func (b *bleNeighborhood) Stop() error {
	close(b.stopped)
	b.device.StopAdvertising()
	b.device.StopScanning()
	return nil
}

func (b *bleNeighborhood) advertiseAndScan() {
	select {
	case <-b.stopped:
		return
	default:
	}

	b.device.Advertise(b.computeAdvertisement())
	b.mu.Lock()
	hasScanner := len(b.scannersById) > 0
	b.mu.Unlock()
	// TODO(bjornick): Don't scan unless there is a scanner running.
	if hasScanner {
		b.device.Scan([]gatt.UUID{}, true)
	}
}

// seenHash returns whether or not we have seen the hash <h> before.
func (b *bleNeighborhood) seenHash(id string, h []byte) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	key := hex.EncodeToString(h)
	entry, ok := b.neighborsHashCache[key]
	if !ok {
		b.pendingHashMap[id] = key
		return false
	}

	if entry.id != id {
		// This can happen either because two different devices chose the same
		// endpoint and name, or that one device changed its mac address.  It
		// seems more likely that the latter happened
		// TODO(bjornick): Deal with the first case.
		entry.id = id
	}
	entry.lastSeen = time.Now()
	return true
}

// getVanadiumHash returns the hash of the vanadium device, if this advertisement
// is from a vanadium device.
func getVanadiumHash(a *gatt.Advertisement) ([]byte, bool) {
	md := a.ManufacturerData
	// The manufacturer data for other vanadium devices contains the hash in
	// the first 8 bytes of the data portion of the packet.  Since we can't tell
	// gatt to only call us for advertisements from a particular manufacturer, we
	// have to decode the manufacturer field to:
	//   1) figure out if this is a vanadium device
	//   2) find the hash of the data.
	// The formnat of the manufacturer data is:
	//    2-bytes for the manufacturer id (in little endian)
	//    1-byte for the length of the data segment
	//    <the actual data>
	if len(md) < 2 {
		return nil, false
	}
	if md[0] != uint8(0xe9) || md[1] != uint8(0x03) {
		return nil, false
	}
	return md[3:], true
}

// gattUUIDtoUUID converts a gatt.UUID to uuid.UUID.
func gattUUIDtoUUID(u gatt.UUID) (uuid.UUID, error) {
	// We can't just do uuid.Parse(u.String()), because the uuid code expects
	// the '-' to be in the string, but gatt.UUID.String() basically does
	// hex.EncodeToString.  Instead we have decode the bytes with the hex
	// decoder and just cast it to a uuid.UUID.
	bytes, err := hex.DecodeString(u.String())
	return uuid.UUID(bytes), err
}

func (b *bleNeighborhood) getAllServices(p gatt.Peripheral) {
	b.mu.Lock()
	h := b.pendingHashMap[p.ID()]
	delete(b.pendingHashMap, p.ID())
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		ch := b.timeoutMap[p.ID()]
		delete(b.timeoutMap, p.ID())
		b.mu.Unlock()
		if ch != nil {
			close(ch)
		}
	}()
	/*
		if err := p.SetMTU(500); err != nil {
			log.Errorf("Failed to set MTU, err: %s", err)
			return
		}
	*/

	ss, err := p.DiscoverServices(nil)

	if err != nil {
		log.Printf("Failed to discover services, err: %s\n", err)
		return
	}

	services := map[string]*bleAdv{}
	for _, s := range ss {
		if s.UUID().Equal(attrGapUuid) {
			continue
		}

		cs, err := p.DiscoverCharacteristics(nil, s)
		if err != nil {
			log.Printf("Failed to discover characteristics: %s\n", err)
			continue
		}

		serviceUuid, err := gattUUIDtoUUID(s.UUID())
		if err != nil {
			log.Printf("Failed to decode uuid: %v\n", err)
			continue
		}

		attrs := map[string][]byte{}
		for _, c := range cs {
			if s.UUID().Equal(attrGattUuid) {
				continue
			}
			u, err := gattUUIDtoUUID(c.UUID())
			if err != nil {
				log.Printf("malformed uuid:%v\n", c.UUID().String())
				continue
			}
			v, err := p.ReadLongCharacteristic(c)
			if err != nil {
				log.Printf("Failed to read the characteristc %s: %v\n", u, err)
				continue
			}
			attrs[u.String()] = v
		}
		services[serviceUuid.String()] = &bleAdv{serviceUuid: serviceUuid, attrs: attrs}
	}

	b.saveDevice(h, p.ID(), services)
}

func (b *bleNeighborhood) startBLEService() error {
	d, err := gatt.NewDevice(gattOptions...)
	if err != nil {
		return err
	}
	onPeriphDiscovered := func(p gatt.Peripheral, a *gatt.Advertisement, rssi int) {
		h, v := getVanadiumHash(a)
		if v && !b.seenHash(p.ID(), h) {
			log.Println("trying to connect to ", p.Name())
			// We stop the scanning and advertising so we can connect to the new device.
			// If one device is changing too frequently we might never find all the devices,
			// since we restart the scan every time we finish connecting, but hopefully
			// that is rare.
			p.Device().StopScanning()
			p.Device().StopAdvertising()
			p.Device().Connect(p)
			b.mu.Lock()
			cancel := make(chan struct{}, 1)
			b.timeoutMap[p.ID()] = cancel
			b.mu.Unlock()
			go func() {
				select {
				case <-time.After(4 * time.Second):
				case <-cancel:
				}
				b.mu.Lock()
				if b.timeoutMap[p.ID()] == cancel {
					delete(b.timeoutMap, p.ID())
				}
				b.mu.Unlock()
				p.Device().CancelConnection(p)
				b.advertiseAndScan()
			}()
		}
	}

	onPeriphConnected := func(p gatt.Peripheral, err error) {
		if err != nil {
			log.Println("Failed to connect:", err)
			return
		}
		b.getAllServices(p)
	}

	onStateChanged := func(d gatt.Device, s gatt.State) {
		log.Printf("State: %s\n", s)
		switch s {
		case gatt.StatePoweredOn:
			defaultServices := addDefaultServices(b.name)
			for k, v := range defaultServices {
				b.services[k] = v
				d.AddService(v)
			}
			b.advertiseAndScan()
		default:
			d.StopScanning()
			d.StopAdvertising()
		}
	}

	d.Handle(
		gatt.CentralConnected(func(c gatt.Central) { log.Printf("Connect: %v\n", c.ID()) }),
		gatt.CentralDisconnected(func(c gatt.Central) { log.Printf("Disconnected: %v\n", c.ID()) }),
		gatt.PeripheralDiscovered(onPeriphDiscovered),
		gatt.PeripheralConnected(onPeriphConnected),
	)

	d.Init(onStateChanged)
	b.device = d
	return nil
}

func (b *bleNeighborhood) saveDevice(hash string, id string, services map[string]*bleAdv) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, found := b.neighborsHashCache[hash]; found {
		log.Printf("Skipping a new save for the same hash (%s)\n",
			hash)
		return
	}

	var oldAdvs map[string]*bleAdv
	if oldEntry, ok := b.knownNeighbors[id]; ok {
		oldAdvs = oldEntry.advertisements
	}
	newEntry := &bleCacheEntry{
		id:             id,
		hash:           hash,
		advertisements: services,
		lastSeen:       time.Now(),
	}
	b.neighborsHashCache[hash] = newEntry
	b.knownNeighbors[id] = newEntry

	for id, newAdv := range services {
		oldAdv := oldAdvs[id]
		if !reflect.DeepEqual(oldAdv, newAdv) {
			uid := uuid.Parse(id)
			for _, s := range b.scannersByService[id] {
				s.handleUpdate(uid, oldAdv, newAdv)
			}
		}
	}
	for id, oldAdv := range oldAdvs {
		if _, exist := services[id]; !exist {
			uid := uuid.Parse(id)
			for _, s := range b.scannersByService[id] {
				s.handleLost(uid, oldAdv)
			}
		}
	}
}

func (b *bleNeighborhood) computeAdvertisement() *gatt.AdvPacket {
	// The hash is:
	// Hash(Hash(name),Hash(b.endpoints))
	hasher := fnv.New64()
	w := func(field string) {
		tmp := fnv.New64()
		tmp.Write([]byte(field))
		hasher.Write(tmp.Sum(nil))
	}
	w(b.name)
	for k, _ := range b.services {
		w(k)
	}
	adv := &gatt.AdvPacket{}
	adv.AppendFlags(0x06)
	adv.AppendManufacturerData(manufacturerId, hasher.Sum(nil))
	adv.AppendName(b.name)
	return adv
}
