// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// This file was auto-generated by the vanadium vdl tool.
// Package: security

package security

import (
	"fmt"
	"reflect"
	"time"
	"v.io/v23/security"
	"v.io/v23/vdl"
	time_2 "v.io/v23/vdlroot/time"
)

var _ = __VDLInit() // Must be first; see __VDLInit comments for details.

//////////////////////////////////////////////////
// Type definitions

type blessingRootsState map[string][]security.BlessingPattern

func (blessingRootsState) __VDLReflect(struct {
	Name string `vdl:"v.io/x/ref/lib/security.blessingRootsState"`
}) {
}

func (m *blessingRootsState) FillVDLTarget(t vdl.Target, tt *vdl.Type) error {
	mapTarget1, err := t.StartMap(tt, len((*m)))
	if err != nil {
		return err
	}
	for key3, value5 := range *m {
		keyTarget2, err := mapTarget1.StartKey()
		if err != nil {
			return err
		}
		if err := keyTarget2.FromString(string(key3), tt.NonOptional().Key()); err != nil {
			return err
		}
		valueTarget4, err := mapTarget1.FinishKeyStartField(keyTarget2)
		if err != nil {
			return err
		}

		listTarget6, err := valueTarget4.StartList(tt.NonOptional().Elem(), len(value5))
		if err != nil {
			return err
		}
		for i, elem8 := range value5 {
			elemTarget7, err := listTarget6.StartElem(i)
			if err != nil {
				return err
			}

			if err := elem8.FillVDLTarget(elemTarget7, tt.NonOptional().Elem().Elem()); err != nil {
				return err
			}
			if err := listTarget6.FinishElem(elemTarget7); err != nil {
				return err
			}
		}
		if err := valueTarget4.FinishList(listTarget6); err != nil {
			return err
		}
		if err := mapTarget1.FinishField(keyTarget2, valueTarget4); err != nil {
			return err
		}
	}
	if err := t.FinishMap(mapTarget1); err != nil {
		return err
	}
	return nil
}

func (m *blessingRootsState) MakeVDLTarget() vdl.Target {
	return &blessingRootsStateTarget{Value: m}
}

type blessingRootsStateTarget struct {
	Value      *blessingRootsState
	currKey    string
	currElem   []security.BlessingPattern
	keyTarget  vdl.StringTarget
	elemTarget __VDLTarget1_list
	vdl.TargetBase
	vdl.MapTargetBase
}

func (t *blessingRootsStateTarget) StartMap(tt *vdl.Type, len int) (vdl.MapTarget, error) {

	if ttWant := vdl.TypeOf((*blessingRootsState)(nil)); !vdl.Compatible(tt, ttWant) {
		return nil, fmt.Errorf("type %v incompatible with %v", tt, ttWant)
	}
	*t.Value = make(blessingRootsState)
	return t, nil
}
func (t *blessingRootsStateTarget) StartKey() (key vdl.Target, _ error) {
	t.currKey = ""
	t.keyTarget.Value = &t.currKey
	target, err := &t.keyTarget, error(nil)
	return target, err
}
func (t *blessingRootsStateTarget) FinishKeyStartField(key vdl.Target) (field vdl.Target, _ error) {
	t.currElem = []security.BlessingPattern(nil)
	t.elemTarget.Value = &t.currElem
	target, err := &t.elemTarget, error(nil)
	return target, err
}
func (t *blessingRootsStateTarget) FinishField(key, field vdl.Target) error {
	(*t.Value)[t.currKey] = t.currElem
	return nil
}
func (t *blessingRootsStateTarget) FinishMap(elem vdl.MapTarget) error {
	if len(*t.Value) == 0 {
		*t.Value = nil
	}

	return nil
}

// []security.BlessingPattern
type __VDLTarget1_list struct {
	Value      *[]security.BlessingPattern
	elemTarget security.BlessingPatternTarget
	vdl.TargetBase
	vdl.ListTargetBase
}

func (t *__VDLTarget1_list) StartList(tt *vdl.Type, len int) (vdl.ListTarget, error) {

	if ttWant := vdl.TypeOf((*[]security.BlessingPattern)(nil)); !vdl.Compatible(tt, ttWant) {
		return nil, fmt.Errorf("type %v incompatible with %v", tt, ttWant)
	}
	if cap(*t.Value) < len {
		*t.Value = make([]security.BlessingPattern, len)
	} else {
		*t.Value = (*t.Value)[:len]
	}
	return t, nil
}
func (t *__VDLTarget1_list) StartElem(index int) (elem vdl.Target, _ error) {
	t.elemTarget.Value = &(*t.Value)[index]
	target, err := &t.elemTarget, error(nil)
	return target, err
}
func (t *__VDLTarget1_list) FinishElem(elem vdl.Target) error {
	return nil
}
func (t *__VDLTarget1_list) FinishList(elem vdl.ListTarget) error {

	return nil
}

func (x *blessingRootsState) VDLRead(dec vdl.Decoder) error {
	var err error
	if err = dec.StartValue(); err != nil {
		return err
	}
	if (dec.StackDepth() == 1 || dec.IsAny()) && !vdl.Compatible(vdl.TypeOf(*x), dec.Type()) {
		return fmt.Errorf("incompatible map %T, from %v", *x, dec.Type())
	}
	var tmpMap blessingRootsState
	if len := dec.LenHint(); len > 0 {
		tmpMap = make(blessingRootsState, len)
	}
	for {
		switch done, err := dec.NextEntry(); {
		case err != nil:
			return err
		case done:
			*x = tmpMap
			return dec.FinishValue()
		}
		var key string
		{
			if err = dec.StartValue(); err != nil {
				return err
			}
			if key, err = dec.DecodeString(); err != nil {
				return err
			}
			if err = dec.FinishValue(); err != nil {
				return err
			}
		}
		var elem []security.BlessingPattern
		{
			if err = __VDLRead1_list(dec, &elem); err != nil {
				return err
			}
		}
		if tmpMap == nil {
			tmpMap = make(blessingRootsState)
		}
		tmpMap[key] = elem
	}
}

func __VDLRead1_list(dec vdl.Decoder, x *[]security.BlessingPattern) error {
	var err error
	if err = dec.StartValue(); err != nil {
		return err
	}
	if (dec.StackDepth() == 1 || dec.IsAny()) && !vdl.Compatible(vdl.TypeOf(*x), dec.Type()) {
		return fmt.Errorf("incompatible list %T, from %v", *x, dec.Type())
	}
	switch len := dec.LenHint(); {
	case len > 0:
		*x = make([]security.BlessingPattern, 0, len)
	default:
		*x = nil
	}
	for {
		switch done, err := dec.NextEntry(); {
		case err != nil:
			return err
		case done:
			return dec.FinishValue()
		}
		var elem security.BlessingPattern
		if err = elem.VDLRead(dec); err != nil {
			return err
		}
		*x = append(*x, elem)
	}
}

func (x blessingRootsState) VDLWrite(enc vdl.Encoder) error {
	if err := enc.StartValue(vdl.TypeOf((*blessingRootsState)(nil))); err != nil {
		return err
	}
	for key, elem := range x {
		if err := enc.StartValue(vdl.TypeOf((*string)(nil))); err != nil {
			return err
		}
		if err := enc.EncodeString(key); err != nil {
			return err
		}
		if err := enc.FinishValue(); err != nil {
			return err
		}
		if err := __VDLWrite1_list(enc, &elem); err != nil {
			return err
		}
	}
	if err := enc.NextEntry(true); err != nil {
		return err
	}
	return enc.FinishValue()
}

func __VDLWrite1_list(enc vdl.Encoder, x *[]security.BlessingPattern) error {
	if err := enc.StartValue(vdl.TypeOf((*[]security.BlessingPattern)(nil))); err != nil {
		return err
	}
	for i := 0; i < len(*x); i++ {
		if err := (*x)[i].VDLWrite(enc); err != nil {
			return err
		}
	}
	if err := enc.NextEntry(true); err != nil {
		return err
	}
	return enc.FinishValue()
}

type dischargeCacheKey [32]byte

func (dischargeCacheKey) __VDLReflect(struct {
	Name string `vdl:"v.io/x/ref/lib/security.dischargeCacheKey"`
}) {
}

func (m *dischargeCacheKey) FillVDLTarget(t vdl.Target, tt *vdl.Type) error {
	if err := t.FromBytes([]byte((*m)[:]), tt); err != nil {
		return err
	}
	return nil
}

func (m *dischargeCacheKey) MakeVDLTarget() vdl.Target {
	return &dischargeCacheKeyTarget{Value: m}
}

type dischargeCacheKeyTarget struct {
	Value *dischargeCacheKey
	vdl.TargetBase
}

func (t *dischargeCacheKeyTarget) FromBytes(src []byte, tt *vdl.Type) error {

	if ttWant := vdl.TypeOf((*dischargeCacheKey)(nil)); !vdl.Compatible(tt, ttWant) {
		return fmt.Errorf("type %v incompatible with %v", tt, ttWant)
	}
	copy((*t.Value)[:], src)

	return nil
}

func (x *dischargeCacheKey) VDLRead(dec vdl.Decoder) error {
	var err error
	if err = dec.StartValue(); err != nil {
		return err
	}
	bytes := x[:]
	if err = dec.DecodeBytes(32, &bytes); err != nil {
		return err
	}
	return dec.FinishValue()
}

func (x dischargeCacheKey) VDLWrite(enc vdl.Encoder) error {
	if err := enc.StartValue(vdl.TypeOf((*dischargeCacheKey)(nil))); err != nil {
		return err
	}
	if err := enc.EncodeBytes([]byte(x[:])); err != nil {
		return err
	}
	return enc.FinishValue()
}

type CachedDischarge struct {
	Discharge security.Discharge
	// CacheTime is the time at which the discharge was first cached.
	CacheTime time.Time
}

func (CachedDischarge) __VDLReflect(struct {
	Name string `vdl:"v.io/x/ref/lib/security.CachedDischarge"`
}) {
}

func (m *CachedDischarge) FillVDLTarget(t vdl.Target, tt *vdl.Type) error {
	fieldsTarget1, err := t.StartFields(tt)
	if err != nil {
		return err
	}
	var wireValue2 security.WireDischarge
	if err := security.WireDischargeFromNative(&wireValue2, m.Discharge); err != nil {
		return err
	}

	var var5 bool
	if field, ok := wireValue2.(security.WireDischargePublicKey); ok {

		var6 := true
		var7 := (field.Value.ThirdPartyCaveatId == "")
		var6 = var6 && var7
		var var8 bool
		if len(field.Value.Caveats) == 0 {
			var8 = true
		}
		var6 = var6 && var8
		var9 := true
		var var10 bool
		if len(field.Value.Signature.Purpose) == 0 {
			var10 = true
		}
		var9 = var9 && var10
		var11 := (field.Value.Signature.Hash == security.Hash(""))
		var9 = var9 && var11
		var var12 bool
		if len(field.Value.Signature.R) == 0 {
			var12 = true
		}
		var9 = var9 && var12
		var var13 bool
		if len(field.Value.Signature.S) == 0 {
			var13 = true
		}
		var9 = var9 && var13
		var6 = var6 && var9
		var5 = var6
	}
	if var5 {
		if err := fieldsTarget1.ZeroField("Discharge"); err != nil && err != vdl.ErrFieldNoExist {
			return err
		}
	} else {
		keyTarget3, fieldTarget4, err := fieldsTarget1.StartField("Discharge")
		if err != vdl.ErrFieldNoExist {
			if err != nil {
				return err
			}

			unionValue14 := wireValue2
			if unionValue14 == nil {
				unionValue14 = security.WireDischargePublicKey{}
			}
			if err := unionValue14.FillVDLTarget(fieldTarget4, tt.NonOptional().Field(0).Type); err != nil {
				return err
			}
			if err := fieldsTarget1.FinishField(keyTarget3, fieldTarget4); err != nil {
				return err
			}
		}
	}
	var wireValue15 time_2.Time
	if err := time_2.TimeFromNative(&wireValue15, m.CacheTime); err != nil {
		return err
	}

	var18 := (wireValue15 == time_2.Time{})
	if var18 {
		if err := fieldsTarget1.ZeroField("CacheTime"); err != nil && err != vdl.ErrFieldNoExist {
			return err
		}
	} else {
		keyTarget16, fieldTarget17, err := fieldsTarget1.StartField("CacheTime")
		if err != vdl.ErrFieldNoExist {
			if err != nil {
				return err
			}

			if err := wireValue15.FillVDLTarget(fieldTarget17, tt.NonOptional().Field(1).Type); err != nil {
				return err
			}
			if err := fieldsTarget1.FinishField(keyTarget16, fieldTarget17); err != nil {
				return err
			}
		}
	}
	if err := t.FinishFields(fieldsTarget1); err != nil {
		return err
	}
	return nil
}

func (m *CachedDischarge) MakeVDLTarget() vdl.Target {
	return &CachedDischargeTarget{Value: m}
}

type CachedDischargeTarget struct {
	Value           *CachedDischarge
	dischargeTarget security.WireDischargeTarget
	cacheTimeTarget time_2.TimeTarget
	vdl.TargetBase
	vdl.FieldsTargetBase
}

func (t *CachedDischargeTarget) StartFields(tt *vdl.Type) (vdl.FieldsTarget, error) {

	if ttWant := vdl.TypeOf((*CachedDischarge)(nil)).Elem(); !vdl.Compatible(tt, ttWant) {
		return nil, fmt.Errorf("type %v incompatible with %v", tt, ttWant)
	}
	return t, nil
}
func (t *CachedDischargeTarget) StartField(name string) (key, field vdl.Target, _ error) {
	switch name {
	case "Discharge":
		t.dischargeTarget.Value = &t.Value.Discharge
		target, err := &t.dischargeTarget, error(nil)
		return nil, target, err
	case "CacheTime":
		t.cacheTimeTarget.Value = &t.Value.CacheTime
		target, err := &t.cacheTimeTarget, error(nil)
		return nil, target, err
	default:
		return nil, nil, fmt.Errorf("field %s not in struct v.io/x/ref/lib/security.CachedDischarge", name)
	}
}
func (t *CachedDischargeTarget) FinishField(_, _ vdl.Target) error {
	return nil
}
func (t *CachedDischargeTarget) ZeroField(name string) error {
	switch name {
	case "Discharge":
		t.Value.Discharge = func() security.Discharge {
			var native security.Discharge
			if err := vdl.Convert(&native, security.WireDischarge(security.WireDischargePublicKey{})); err != nil {
				panic(err)
			}
			return native
		}()
		return nil
	case "CacheTime":
		t.Value.CacheTime = func() time.Time {
			var native time.Time
			if err := vdl.Convert(&native, time_2.Time{}); err != nil {
				panic(err)
			}
			return native
		}()
		return nil
	default:
		return fmt.Errorf("field %s not in struct v.io/x/ref/lib/security.CachedDischarge", name)
	}
}
func (t *CachedDischargeTarget) FinishFields(_ vdl.FieldsTarget) error {

	return nil
}

func (x *CachedDischarge) VDLRead(dec vdl.Decoder) error {
	*x = CachedDischarge{
		Discharge: func() security.Discharge {
			var native security.Discharge
			if err := vdl.Convert(&native, security.WireDischarge(security.WireDischargePublicKey{})); err != nil {
				panic(err)
			}
			return native
		}(),
	}
	var err error
	if err = dec.StartValue(); err != nil {
		return err
	}
	if (dec.StackDepth() == 1 || dec.IsAny()) && !vdl.Compatible(vdl.TypeOf(*x), dec.Type()) {
		return fmt.Errorf("incompatible struct %T, from %v", *x, dec.Type())
	}
	for {
		f, err := dec.NextField()
		if err != nil {
			return err
		}
		switch f {
		case "":
			return dec.FinishValue()
		case "Discharge":
			var wire security.WireDischarge
			if err = security.VDLReadWireDischarge(dec, &wire); err != nil {
				return err
			}
			if err = security.WireDischargeToNative(wire, &x.Discharge); err != nil {
				return err
			}
		case "CacheTime":
			var wire time_2.Time
			if err = wire.VDLRead(dec); err != nil {
				return err
			}
			if err = time_2.TimeToNative(wire, &x.CacheTime); err != nil {
				return err
			}
		default:
			if err = dec.SkipValue(); err != nil {
				return err
			}
		}
	}
}

func (x CachedDischarge) VDLWrite(enc vdl.Encoder) error {
	if err := enc.StartValue(vdl.TypeOf((*CachedDischarge)(nil)).Elem()); err != nil {
		return err
	}
	var wireValue1 security.WireDischarge
	if err := security.WireDischargeFromNative(&wireValue1, x.Discharge); err != nil {
		return fmt.Errorf("error converting x.Discharge to wiretype")
	}

	var var2 bool
	if field, ok := wireValue1.(security.WireDischargePublicKey); ok {

		var3 := true
		var4 := (field.Value.ThirdPartyCaveatId == "")
		var3 = var3 && var4
		var var5 bool
		if len(field.Value.Caveats) == 0 {
			var5 = true
		}
		var3 = var3 && var5
		var6 := true
		var var7 bool
		if len(field.Value.Signature.Purpose) == 0 {
			var7 = true
		}
		var6 = var6 && var7
		var8 := (field.Value.Signature.Hash == security.Hash(""))
		var6 = var6 && var8
		var var9 bool
		if len(field.Value.Signature.R) == 0 {
			var9 = true
		}
		var6 = var6 && var9
		var var10 bool
		if len(field.Value.Signature.S) == 0 {
			var10 = true
		}
		var6 = var6 && var10
		var3 = var3 && var6
		var2 = var3
	}
	if !(var2) {
		if err := enc.NextField("Discharge"); err != nil {
			return err
		}
		var wire security.WireDischarge
		if err := security.WireDischargeFromNative(&wire, x.Discharge); err != nil {
			return err
		}
		if err := wire.VDLWrite(enc); err != nil {
			return err
		}
	}
	var wireValue11 time_2.Time
	if err := time_2.TimeFromNative(&wireValue11, x.CacheTime); err != nil {
		return fmt.Errorf("error converting x.CacheTime to wiretype")
	}

	var12 := (wireValue11 == time_2.Time{})
	if !(var12) {
		if err := enc.NextField("CacheTime"); err != nil {
			return err
		}
		var wire time_2.Time
		if err := time_2.TimeFromNative(&wire, x.CacheTime); err != nil {
			return err
		}
		if err := wire.VDLWrite(enc); err != nil {
			return err
		}
	}
	if err := enc.NextField(""); err != nil {
		return err
	}
	return enc.FinishValue()
}

type blessingStoreState struct {
	// PeerBlessings maps BlessingPatterns to the Blessings object that is to
	// be shared with peers which present blessings of their own that match the
	// pattern.
	//
	// All blessings bind to the same public key.
	PeerBlessings map[security.BlessingPattern]security.Blessings
	// DefaultBlessings is the default Blessings to be shared with peers for which
	// no other information is available to select blessings.
	DefaultBlessings security.Blessings
	// DischargeCache is the cache of discharges.
	// TODO(mattr): This map is deprecated in favor of the Discharges map below.
	DischargeCache map[dischargeCacheKey]security.Discharge
	// DischargeCache is the cache of discharges.
	Discharges map[dischargeCacheKey]CachedDischarge
	// CacheKeyFormat is the dischargeCacheKey format version. It should incremented
	// any time the format of the dischargeCacheKey is changed.
	CacheKeyFormat uint32
}

func (blessingStoreState) __VDLReflect(struct {
	Name string `vdl:"v.io/x/ref/lib/security.blessingStoreState"`
}) {
}

func (m *blessingStoreState) FillVDLTarget(t vdl.Target, tt *vdl.Type) error {
	fieldsTarget1, err := t.StartFields(tt)
	if err != nil {
		return err
	}
	var var4 bool
	if len(m.PeerBlessings) == 0 {
		var4 = true
	}
	if var4 {
		if err := fieldsTarget1.ZeroField("PeerBlessings"); err != nil && err != vdl.ErrFieldNoExist {
			return err
		}
	} else {
		keyTarget2, fieldTarget3, err := fieldsTarget1.StartField("PeerBlessings")
		if err != vdl.ErrFieldNoExist {
			if err != nil {
				return err
			}

			mapTarget5, err := fieldTarget3.StartMap(tt.NonOptional().Field(0).Type, len(m.PeerBlessings))
			if err != nil {
				return err
			}
			for key7, value9 := range m.PeerBlessings {
				keyTarget6, err := mapTarget5.StartKey()
				if err != nil {
					return err
				}

				if err := key7.FillVDLTarget(keyTarget6, tt.NonOptional().Field(0).Type.Key()); err != nil {
					return err
				}
				valueTarget8, err := mapTarget5.FinishKeyStartField(keyTarget6)
				if err != nil {
					return err
				}

				var wireValue10 security.WireBlessings
				if err := security.WireBlessingsFromNative(&wireValue10, value9); err != nil {
					return err
				}

				if err := wireValue10.FillVDLTarget(valueTarget8, tt.NonOptional().Field(0).Type.Elem()); err != nil {
					return err
				}
				if err := mapTarget5.FinishField(keyTarget6, valueTarget8); err != nil {
					return err
				}
			}
			if err := fieldTarget3.FinishMap(mapTarget5); err != nil {
				return err
			}
			if err := fieldsTarget1.FinishField(keyTarget2, fieldTarget3); err != nil {
				return err
			}
		}
	}
	var wireValue11 security.WireBlessings
	if err := security.WireBlessingsFromNative(&wireValue11, m.DefaultBlessings); err != nil {
		return err
	}

	var14 := true
	var var15 bool
	if len(wireValue11.CertificateChains) == 0 {
		var15 = true
	}
	var14 = var14 && var15
	if var14 {
		if err := fieldsTarget1.ZeroField("DefaultBlessings"); err != nil && err != vdl.ErrFieldNoExist {
			return err
		}
	} else {
		keyTarget12, fieldTarget13, err := fieldsTarget1.StartField("DefaultBlessings")
		if err != vdl.ErrFieldNoExist {
			if err != nil {
				return err
			}

			if err := wireValue11.FillVDLTarget(fieldTarget13, tt.NonOptional().Field(1).Type); err != nil {
				return err
			}
			if err := fieldsTarget1.FinishField(keyTarget12, fieldTarget13); err != nil {
				return err
			}
		}
	}
	var var18 bool
	if len(m.DischargeCache) == 0 {
		var18 = true
	}
	if var18 {
		if err := fieldsTarget1.ZeroField("DischargeCache"); err != nil && err != vdl.ErrFieldNoExist {
			return err
		}
	} else {
		keyTarget16, fieldTarget17, err := fieldsTarget1.StartField("DischargeCache")
		if err != vdl.ErrFieldNoExist {
			if err != nil {
				return err
			}

			mapTarget19, err := fieldTarget17.StartMap(tt.NonOptional().Field(2).Type, len(m.DischargeCache))
			if err != nil {
				return err
			}
			for key21, value23 := range m.DischargeCache {
				keyTarget20, err := mapTarget19.StartKey()
				if err != nil {
					return err
				}

				if err := key21.FillVDLTarget(keyTarget20, tt.NonOptional().Field(2).Type.Key()); err != nil {
					return err
				}
				valueTarget22, err := mapTarget19.FinishKeyStartField(keyTarget20)
				if err != nil {
					return err
				}

				var wireValue24 security.WireDischarge
				if err := security.WireDischargeFromNative(&wireValue24, value23); err != nil {
					return err
				}

				unionValue25 := wireValue24
				if unionValue25 == nil {
					unionValue25 = security.WireDischargePublicKey{}
				}
				if err := unionValue25.FillVDLTarget(valueTarget22, tt.NonOptional().Field(2).Type.Elem()); err != nil {
					return err
				}
				if err := mapTarget19.FinishField(keyTarget20, valueTarget22); err != nil {
					return err
				}
			}
			if err := fieldTarget17.FinishMap(mapTarget19); err != nil {
				return err
			}
			if err := fieldsTarget1.FinishField(keyTarget16, fieldTarget17); err != nil {
				return err
			}
		}
	}
	var var28 bool
	if len(m.Discharges) == 0 {
		var28 = true
	}
	if var28 {
		if err := fieldsTarget1.ZeroField("Discharges"); err != nil && err != vdl.ErrFieldNoExist {
			return err
		}
	} else {
		keyTarget26, fieldTarget27, err := fieldsTarget1.StartField("Discharges")
		if err != vdl.ErrFieldNoExist {
			if err != nil {
				return err
			}

			mapTarget29, err := fieldTarget27.StartMap(tt.NonOptional().Field(3).Type, len(m.Discharges))
			if err != nil {
				return err
			}
			for key31, value33 := range m.Discharges {
				keyTarget30, err := mapTarget29.StartKey()
				if err != nil {
					return err
				}

				if err := key31.FillVDLTarget(keyTarget30, tt.NonOptional().Field(3).Type.Key()); err != nil {
					return err
				}
				valueTarget32, err := mapTarget29.FinishKeyStartField(keyTarget30)
				if err != nil {
					return err
				}

				if err := value33.FillVDLTarget(valueTarget32, tt.NonOptional().Field(3).Type.Elem()); err != nil {
					return err
				}
				if err := mapTarget29.FinishField(keyTarget30, valueTarget32); err != nil {
					return err
				}
			}
			if err := fieldTarget27.FinishMap(mapTarget29); err != nil {
				return err
			}
			if err := fieldsTarget1.FinishField(keyTarget26, fieldTarget27); err != nil {
				return err
			}
		}
	}
	var36 := (m.CacheKeyFormat == uint32(0))
	if var36 {
		if err := fieldsTarget1.ZeroField("CacheKeyFormat"); err != nil && err != vdl.ErrFieldNoExist {
			return err
		}
	} else {
		keyTarget34, fieldTarget35, err := fieldsTarget1.StartField("CacheKeyFormat")
		if err != vdl.ErrFieldNoExist {
			if err != nil {
				return err
			}
			if err := fieldTarget35.FromUint(uint64(m.CacheKeyFormat), tt.NonOptional().Field(4).Type); err != nil {
				return err
			}
			if err := fieldsTarget1.FinishField(keyTarget34, fieldTarget35); err != nil {
				return err
			}
		}
	}
	if err := t.FinishFields(fieldsTarget1); err != nil {
		return err
	}
	return nil
}

func (m *blessingStoreState) MakeVDLTarget() vdl.Target {
	return &blessingStoreStateTarget{Value: m}
}

type blessingStoreStateTarget struct {
	Value                  *blessingStoreState
	peerBlessingsTarget    __VDLTarget2_map
	defaultBlessingsTarget security.WireBlessingsTarget
	dischargeCacheTarget   __VDLTarget3_map
	dischargesTarget       __VDLTarget4_map
	cacheKeyFormatTarget   vdl.Uint32Target
	vdl.TargetBase
	vdl.FieldsTargetBase
}

func (t *blessingStoreStateTarget) StartFields(tt *vdl.Type) (vdl.FieldsTarget, error) {

	if ttWant := vdl.TypeOf((*blessingStoreState)(nil)).Elem(); !vdl.Compatible(tt, ttWant) {
		return nil, fmt.Errorf("type %v incompatible with %v", tt, ttWant)
	}
	return t, nil
}
func (t *blessingStoreStateTarget) StartField(name string) (key, field vdl.Target, _ error) {
	switch name {
	case "PeerBlessings":
		t.peerBlessingsTarget.Value = &t.Value.PeerBlessings
		target, err := &t.peerBlessingsTarget, error(nil)
		return nil, target, err
	case "DefaultBlessings":
		t.defaultBlessingsTarget.Value = &t.Value.DefaultBlessings
		target, err := &t.defaultBlessingsTarget, error(nil)
		return nil, target, err
	case "DischargeCache":
		t.dischargeCacheTarget.Value = &t.Value.DischargeCache
		target, err := &t.dischargeCacheTarget, error(nil)
		return nil, target, err
	case "Discharges":
		t.dischargesTarget.Value = &t.Value.Discharges
		target, err := &t.dischargesTarget, error(nil)
		return nil, target, err
	case "CacheKeyFormat":
		t.cacheKeyFormatTarget.Value = &t.Value.CacheKeyFormat
		target, err := &t.cacheKeyFormatTarget, error(nil)
		return nil, target, err
	default:
		return nil, nil, fmt.Errorf("field %s not in struct v.io/x/ref/lib/security.blessingStoreState", name)
	}
}
func (t *blessingStoreStateTarget) FinishField(_, _ vdl.Target) error {
	return nil
}
func (t *blessingStoreStateTarget) ZeroField(name string) error {
	switch name {
	case "PeerBlessings":
		t.Value.PeerBlessings = map[security.BlessingPattern]security.Blessings(nil)
		return nil
	case "DefaultBlessings":
		t.Value.DefaultBlessings = func() security.Blessings {
			var native security.Blessings
			if err := vdl.Convert(&native, security.WireBlessings{}); err != nil {
				panic(err)
			}
			return native
		}()
		return nil
	case "DischargeCache":
		t.Value.DischargeCache = map[dischargeCacheKey]security.Discharge(nil)
		return nil
	case "Discharges":
		t.Value.Discharges = map[dischargeCacheKey]CachedDischarge(nil)
		return nil
	case "CacheKeyFormat":
		t.Value.CacheKeyFormat = uint32(0)
		return nil
	default:
		return fmt.Errorf("field %s not in struct v.io/x/ref/lib/security.blessingStoreState", name)
	}
}
func (t *blessingStoreStateTarget) FinishFields(_ vdl.FieldsTarget) error {

	return nil
}

// map[security.BlessingPattern]security.Blessings
type __VDLTarget2_map struct {
	Value      *map[security.BlessingPattern]security.Blessings
	currKey    security.BlessingPattern
	currElem   security.Blessings
	keyTarget  security.BlessingPatternTarget
	elemTarget security.WireBlessingsTarget
	vdl.TargetBase
	vdl.MapTargetBase
}

func (t *__VDLTarget2_map) StartMap(tt *vdl.Type, len int) (vdl.MapTarget, error) {

	if ttWant := vdl.TypeOf((*map[security.BlessingPattern]security.Blessings)(nil)); !vdl.Compatible(tt, ttWant) {
		return nil, fmt.Errorf("type %v incompatible with %v", tt, ttWant)
	}
	*t.Value = make(map[security.BlessingPattern]security.Blessings)
	return t, nil
}
func (t *__VDLTarget2_map) StartKey() (key vdl.Target, _ error) {
	t.currKey = security.BlessingPattern("")
	t.keyTarget.Value = &t.currKey
	target, err := &t.keyTarget, error(nil)
	return target, err
}
func (t *__VDLTarget2_map) FinishKeyStartField(key vdl.Target) (field vdl.Target, _ error) {
	t.currElem = reflect.Zero(reflect.TypeOf(t.currElem)).Interface().(security.Blessings)
	t.elemTarget.Value = &t.currElem
	target, err := &t.elemTarget, error(nil)
	return target, err
}
func (t *__VDLTarget2_map) FinishField(key, field vdl.Target) error {
	(*t.Value)[t.currKey] = t.currElem
	return nil
}
func (t *__VDLTarget2_map) FinishMap(elem vdl.MapTarget) error {
	if len(*t.Value) == 0 {
		*t.Value = nil
	}

	return nil
}

// map[dischargeCacheKey]security.Discharge
type __VDLTarget3_map struct {
	Value      *map[dischargeCacheKey]security.Discharge
	currKey    dischargeCacheKey
	currElem   security.Discharge
	keyTarget  dischargeCacheKeyTarget
	elemTarget security.WireDischargeTarget
	vdl.TargetBase
	vdl.MapTargetBase
}

func (t *__VDLTarget3_map) StartMap(tt *vdl.Type, len int) (vdl.MapTarget, error) {

	if ttWant := vdl.TypeOf((*map[dischargeCacheKey]security.Discharge)(nil)); !vdl.Compatible(tt, ttWant) {
		return nil, fmt.Errorf("type %v incompatible with %v", tt, ttWant)
	}
	*t.Value = make(map[dischargeCacheKey]security.Discharge)
	return t, nil
}
func (t *__VDLTarget3_map) StartKey() (key vdl.Target, _ error) {
	t.currKey = dischargeCacheKey{}
	t.keyTarget.Value = &t.currKey
	target, err := &t.keyTarget, error(nil)
	return target, err
}
func (t *__VDLTarget3_map) FinishKeyStartField(key vdl.Target) (field vdl.Target, _ error) {
	t.currElem = reflect.Zero(reflect.TypeOf(t.currElem)).Interface().(security.Discharge)
	t.elemTarget.Value = &t.currElem
	target, err := &t.elemTarget, error(nil)
	return target, err
}
func (t *__VDLTarget3_map) FinishField(key, field vdl.Target) error {
	(*t.Value)[t.currKey] = t.currElem
	return nil
}
func (t *__VDLTarget3_map) FinishMap(elem vdl.MapTarget) error {
	if len(*t.Value) == 0 {
		*t.Value = nil
	}

	return nil
}

// map[dischargeCacheKey]CachedDischarge
type __VDLTarget4_map struct {
	Value      *map[dischargeCacheKey]CachedDischarge
	currKey    dischargeCacheKey
	currElem   CachedDischarge
	keyTarget  dischargeCacheKeyTarget
	elemTarget CachedDischargeTarget
	vdl.TargetBase
	vdl.MapTargetBase
}

func (t *__VDLTarget4_map) StartMap(tt *vdl.Type, len int) (vdl.MapTarget, error) {

	if ttWant := vdl.TypeOf((*map[dischargeCacheKey]CachedDischarge)(nil)); !vdl.Compatible(tt, ttWant) {
		return nil, fmt.Errorf("type %v incompatible with %v", tt, ttWant)
	}
	*t.Value = make(map[dischargeCacheKey]CachedDischarge)
	return t, nil
}
func (t *__VDLTarget4_map) StartKey() (key vdl.Target, _ error) {
	t.currKey = dischargeCacheKey{}
	t.keyTarget.Value = &t.currKey
	target, err := &t.keyTarget, error(nil)
	return target, err
}
func (t *__VDLTarget4_map) FinishKeyStartField(key vdl.Target) (field vdl.Target, _ error) {
	t.currElem = reflect.Zero(reflect.TypeOf(t.currElem)).Interface().(CachedDischarge)
	t.elemTarget.Value = &t.currElem
	target, err := &t.elemTarget, error(nil)
	return target, err
}
func (t *__VDLTarget4_map) FinishField(key, field vdl.Target) error {
	(*t.Value)[t.currKey] = t.currElem
	return nil
}
func (t *__VDLTarget4_map) FinishMap(elem vdl.MapTarget) error {
	if len(*t.Value) == 0 {
		*t.Value = nil
	}

	return nil
}

func (x *blessingStoreState) VDLRead(dec vdl.Decoder) error {
	*x = blessingStoreState{}
	var err error
	if err = dec.StartValue(); err != nil {
		return err
	}
	if (dec.StackDepth() == 1 || dec.IsAny()) && !vdl.Compatible(vdl.TypeOf(*x), dec.Type()) {
		return fmt.Errorf("incompatible struct %T, from %v", *x, dec.Type())
	}
	for {
		f, err := dec.NextField()
		if err != nil {
			return err
		}
		switch f {
		case "":
			return dec.FinishValue()
		case "PeerBlessings":
			if err = __VDLRead2_map(dec, &x.PeerBlessings); err != nil {
				return err
			}
		case "DefaultBlessings":
			var wire security.WireBlessings
			if err = wire.VDLRead(dec); err != nil {
				return err
			}
			if err = security.WireBlessingsToNative(wire, &x.DefaultBlessings); err != nil {
				return err
			}
		case "DischargeCache":
			if err = __VDLRead3_map(dec, &x.DischargeCache); err != nil {
				return err
			}
		case "Discharges":
			if err = __VDLRead4_map(dec, &x.Discharges); err != nil {
				return err
			}
		case "CacheKeyFormat":
			if err = dec.StartValue(); err != nil {
				return err
			}
			tmp, err := dec.DecodeUint(32)
			if err != nil {
				return err
			}
			x.CacheKeyFormat = uint32(tmp)
			if err = dec.FinishValue(); err != nil {
				return err
			}
		default:
			if err = dec.SkipValue(); err != nil {
				return err
			}
		}
	}
}

func __VDLRead2_map(dec vdl.Decoder, x *map[security.BlessingPattern]security.Blessings) error {
	var err error
	if err = dec.StartValue(); err != nil {
		return err
	}
	if (dec.StackDepth() == 1 || dec.IsAny()) && !vdl.Compatible(vdl.TypeOf(*x), dec.Type()) {
		return fmt.Errorf("incompatible map %T, from %v", *x, dec.Type())
	}
	var tmpMap map[security.BlessingPattern]security.Blessings
	if len := dec.LenHint(); len > 0 {
		tmpMap = make(map[security.BlessingPattern]security.Blessings, len)
	}
	for {
		switch done, err := dec.NextEntry(); {
		case err != nil:
			return err
		case done:
			*x = tmpMap
			return dec.FinishValue()
		}
		var key security.BlessingPattern
		{
			if err = key.VDLRead(dec); err != nil {
				return err
			}
		}
		var elem security.Blessings
		{
			var wire security.WireBlessings
			if err = wire.VDLRead(dec); err != nil {
				return err
			}
			if err = security.WireBlessingsToNative(wire, &elem); err != nil {
				return err
			}
		}
		if tmpMap == nil {
			tmpMap = make(map[security.BlessingPattern]security.Blessings)
		}
		tmpMap[key] = elem
	}
}

func __VDLRead3_map(dec vdl.Decoder, x *map[dischargeCacheKey]security.Discharge) error {
	var err error
	if err = dec.StartValue(); err != nil {
		return err
	}
	if (dec.StackDepth() == 1 || dec.IsAny()) && !vdl.Compatible(vdl.TypeOf(*x), dec.Type()) {
		return fmt.Errorf("incompatible map %T, from %v", *x, dec.Type())
	}
	var tmpMap map[dischargeCacheKey]security.Discharge
	if len := dec.LenHint(); len > 0 {
		tmpMap = make(map[dischargeCacheKey]security.Discharge, len)
	}
	for {
		switch done, err := dec.NextEntry(); {
		case err != nil:
			return err
		case done:
			*x = tmpMap
			return dec.FinishValue()
		}
		var key dischargeCacheKey
		{
			if err = key.VDLRead(dec); err != nil {
				return err
			}
		}
		var elem security.Discharge
		{
			var wire security.WireDischarge
			if err = security.VDLReadWireDischarge(dec, &wire); err != nil {
				return err
			}
			if err = security.WireDischargeToNative(wire, &elem); err != nil {
				return err
			}
		}
		if tmpMap == nil {
			tmpMap = make(map[dischargeCacheKey]security.Discharge)
		}
		tmpMap[key] = elem
	}
}

func __VDLRead4_map(dec vdl.Decoder, x *map[dischargeCacheKey]CachedDischarge) error {
	var err error
	if err = dec.StartValue(); err != nil {
		return err
	}
	if (dec.StackDepth() == 1 || dec.IsAny()) && !vdl.Compatible(vdl.TypeOf(*x), dec.Type()) {
		return fmt.Errorf("incompatible map %T, from %v", *x, dec.Type())
	}
	var tmpMap map[dischargeCacheKey]CachedDischarge
	if len := dec.LenHint(); len > 0 {
		tmpMap = make(map[dischargeCacheKey]CachedDischarge, len)
	}
	for {
		switch done, err := dec.NextEntry(); {
		case err != nil:
			return err
		case done:
			*x = tmpMap
			return dec.FinishValue()
		}
		var key dischargeCacheKey
		{
			if err = key.VDLRead(dec); err != nil {
				return err
			}
		}
		var elem CachedDischarge
		{
			if err = elem.VDLRead(dec); err != nil {
				return err
			}
		}
		if tmpMap == nil {
			tmpMap = make(map[dischargeCacheKey]CachedDischarge)
		}
		tmpMap[key] = elem
	}
}

func (x blessingStoreState) VDLWrite(enc vdl.Encoder) error {
	if err := enc.StartValue(vdl.TypeOf((*blessingStoreState)(nil)).Elem()); err != nil {
		return err
	}
	var var1 bool
	if len(x.PeerBlessings) == 0 {
		var1 = true
	}
	if !(var1) {
		if err := enc.NextField("PeerBlessings"); err != nil {
			return err
		}
		if err := __VDLWrite2_map(enc, &x.PeerBlessings); err != nil {
			return err
		}
	}
	var wireValue2 security.WireBlessings
	if err := security.WireBlessingsFromNative(&wireValue2, x.DefaultBlessings); err != nil {
		return fmt.Errorf("error converting x.DefaultBlessings to wiretype")
	}

	var3 := true
	var var4 bool
	if len(wireValue2.CertificateChains) == 0 {
		var4 = true
	}
	var3 = var3 && var4
	if !(var3) {
		if err := enc.NextField("DefaultBlessings"); err != nil {
			return err
		}
		var wire security.WireBlessings
		if err := security.WireBlessingsFromNative(&wire, x.DefaultBlessings); err != nil {
			return err
		}
		if err := wire.VDLWrite(enc); err != nil {
			return err
		}
	}
	var var5 bool
	if len(x.DischargeCache) == 0 {
		var5 = true
	}
	if !(var5) {
		if err := enc.NextField("DischargeCache"); err != nil {
			return err
		}
		if err := __VDLWrite3_map(enc, &x.DischargeCache); err != nil {
			return err
		}
	}
	var var6 bool
	if len(x.Discharges) == 0 {
		var6 = true
	}
	if !(var6) {
		if err := enc.NextField("Discharges"); err != nil {
			return err
		}
		if err := __VDLWrite4_map(enc, &x.Discharges); err != nil {
			return err
		}
	}
	var7 := (x.CacheKeyFormat == uint32(0))
	if !(var7) {
		if err := enc.NextField("CacheKeyFormat"); err != nil {
			return err
		}
		if err := enc.StartValue(vdl.TypeOf((*uint32)(nil))); err != nil {
			return err
		}
		if err := enc.EncodeUint(uint64(x.CacheKeyFormat)); err != nil {
			return err
		}
		if err := enc.FinishValue(); err != nil {
			return err
		}
	}
	if err := enc.NextField(""); err != nil {
		return err
	}
	return enc.FinishValue()
}

func __VDLWrite2_map(enc vdl.Encoder, x *map[security.BlessingPattern]security.Blessings) error {
	if err := enc.StartValue(vdl.TypeOf((*map[security.BlessingPattern]security.Blessings)(nil))); err != nil {
		return err
	}
	for key, elem := range *x {
		if err := key.VDLWrite(enc); err != nil {
			return err
		}
		var wire security.WireBlessings
		if err := security.WireBlessingsFromNative(&wire, elem); err != nil {
			return err
		}
		if err := wire.VDLWrite(enc); err != nil {
			return err
		}
	}
	if err := enc.NextEntry(true); err != nil {
		return err
	}
	return enc.FinishValue()
}

func __VDLWrite3_map(enc vdl.Encoder, x *map[dischargeCacheKey]security.Discharge) error {
	if err := enc.StartValue(vdl.TypeOf((*map[dischargeCacheKey]security.Discharge)(nil))); err != nil {
		return err
	}
	for key, elem := range *x {
		if err := key.VDLWrite(enc); err != nil {
			return err
		}
		var wire security.WireDischarge
		if err := security.WireDischargeFromNative(&wire, elem); err != nil {
			return err
		}
		if err := wire.VDLWrite(enc); err != nil {
			return err
		}
	}
	if err := enc.NextEntry(true); err != nil {
		return err
	}
	return enc.FinishValue()
}

func __VDLWrite4_map(enc vdl.Encoder, x *map[dischargeCacheKey]CachedDischarge) error {
	if err := enc.StartValue(vdl.TypeOf((*map[dischargeCacheKey]CachedDischarge)(nil))); err != nil {
		return err
	}
	for key, elem := range *x {
		if err := key.VDLWrite(enc); err != nil {
			return err
		}
		if err := elem.VDLWrite(enc); err != nil {
			return err
		}
	}
	if err := enc.NextEntry(true); err != nil {
		return err
	}
	return enc.FinishValue()
}

var __VDLInitCalled bool

// __VDLInit performs vdl initialization.  It is safe to call multiple times.
// If you have an init ordering issue, just insert the following line verbatim
// into your source files in this package, right after the "package foo" clause:
//
//    var _ = __VDLInit()
//
// The purpose of this function is to ensure that vdl initialization occurs in
// the right order, and very early in the init sequence.  In particular, vdl
// registration and package variable initialization needs to occur before
// functions like vdl.TypeOf will work properly.
//
// This function returns a dummy value, so that it can be used to initialize the
// first var in the file, to take advantage of Go's defined init order.
func __VDLInit() struct{} {
	if __VDLInitCalled {
		return struct{}{}
	}
	__VDLInitCalled = true

	// Register types.
	vdl.Register((*blessingRootsState)(nil))
	vdl.Register((*dischargeCacheKey)(nil))
	vdl.Register((*CachedDischarge)(nil))
	vdl.Register((*blessingStoreState)(nil))

	return struct{}{}
}
