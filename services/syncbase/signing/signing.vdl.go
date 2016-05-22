// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// This file was auto-generated by the vanadium vdl tool.
// Package: signing

package signing

import (
	"fmt"
	"v.io/v23/security"
	"v.io/v23/vdl"
)

var _ = __VDLInit() // Must be first; see __VDLInit comments for details.

//////////////////////////////////////////////////
// Type definitions

type (
	// Item represents any single field of the Item union type.
	//
	// An Item represents either a marshalled data item or its SHA-256 hash.
	// The Data field is a []byte, rather than an "any" to make signatures
	// determistic.  VOM encoding is not deterministic for two reasons:
	// - map elements may be marshalled in any order
	// - different versions of VOM may marshal in different ways.
	// Thus, the initial producer of a data item marshals the data once, and it is
	// this marshalled form that is transmitted from device to device.  If the
	// data were unmarshalled and then remarsahalled, the signatures might not
	// match.  The Hash field is used instead of the Data field when the recipient
	// of the DataWithSignature is not permitted to see certain Items' Data
	// fields.
	Item interface {
		// Index returns the field index.
		Index() int
		// Interface returns the field value as an interface.
		Interface() interface{}
		// Name returns the field name.
		Name() string
		// __VDLReflect describes the Item union type.
		__VDLReflect(__ItemReflect)
		VDLIsZero() bool
		VDLWrite(vdl.Encoder) error
	}
	// ItemData represents field Data of the Item union type.
	ItemData struct{ Value []byte } // Marshalled form of data.
	// ItemHash represents field Hash of the Item union type.
	ItemHash struct{ Value []byte } // Hash of what would have been in Data, as returned by SumByteVectorWithLength(Data).
	// __ItemReflect describes the Item union type.
	__ItemReflect struct {
		Name  string `vdl:"v.io/x/ref/services/syncbase/signing.Item"`
		Type  Item
		Union struct {
			Data ItemData
			Hash ItemHash
		}
	}
)

func (x ItemData) Index() int                 { return 0 }
func (x ItemData) Interface() interface{}     { return x.Value }
func (x ItemData) Name() string               { return "Data" }
func (x ItemData) __VDLReflect(__ItemReflect) {}

func (x ItemHash) Index() int                 { return 1 }
func (x ItemHash) Interface() interface{}     { return x.Value }
func (x ItemHash) Name() string               { return "Hash" }
func (x ItemHash) __VDLReflect(__ItemReflect) {}

func (x ItemData) VDLIsZero() bool {
	return len(x.Value) == 0
}

func (x ItemHash) VDLIsZero() bool {
	return false
}

func (x ItemData) VDLWrite(enc vdl.Encoder) error {
	if err := enc.StartValue(__VDLType_union_2); err != nil {
		return err
	}
	if err := enc.NextField("Data"); err != nil {
		return err
	}
	if err := enc.StartValue(__VDLType_list_1); err != nil {
		return err
	}
	if err := enc.EncodeBytes(x.Value); err != nil {
		return err
	}
	if err := enc.FinishValue(); err != nil {
		return err
	}
	if err := enc.NextField(""); err != nil {
		return err
	}
	return enc.FinishValue()
}

func (x ItemHash) VDLWrite(enc vdl.Encoder) error {
	if err := enc.StartValue(__VDLType_union_2); err != nil {
		return err
	}
	if err := enc.NextField("Hash"); err != nil {
		return err
	}
	if err := enc.StartValue(__VDLType_list_1); err != nil {
		return err
	}
	if err := enc.EncodeBytes(x.Value); err != nil {
		return err
	}
	if err := enc.FinishValue(); err != nil {
		return err
	}
	if err := enc.NextField(""); err != nil {
		return err
	}
	return enc.FinishValue()
}

func VDLReadItem(dec vdl.Decoder, x *Item) error {
	if err := dec.StartValue(__VDLType_union_2); err != nil {
		return err
	}
	f, err := dec.NextField()
	if err != nil {
		return err
	}
	switch f {
	case "Data":
		var field ItemData
		if err := dec.ReadValueBytes(-1, &field.Value); err != nil {
			return err
		}
		*x = field
	case "Hash":
		var field ItemHash
		if err := dec.ReadValueBytes(-1, &field.Value); err != nil {
			return err
		}
		*x = field
	case "":
		return fmt.Errorf("missing field in union %T, from %v", x, dec.Type())
	default:
		return fmt.Errorf("field %q not in union %T, from %v", f, x, dec.Type())
	}
	switch f, err := dec.NextField(); {
	case err != nil:
		return err
	case f != "":
		return fmt.Errorf("extra field %q in union %T, from %v", f, x, dec.Type())
	}
	return dec.FinishValue()
}

// A DataWithSignature represents a signed, and possibily validated, collection
// of Item structs.
//
// If IsValidated==false and the AuthorSigned signature is valid, it means:
//    The signer whose Blessings have hash BlessingsHash asserts Data.
//
// If IsValidated==true and both AuthorSigned and ValidatorSigned signatures are is valid,
// it means both:
// 1) The signer whose Blessings b have hash BlessingsHash asserts Data.
// 2) If vd is the ValidatorData with hash ValidatorDataHash, the owner of
//    vd.PublicKey asserts that it checked that at least the names vd.Names[] were
//    valid in b.
//
// The sender obtains:
// - BlessingsHash (and the wire form of the blessings) with ValidationCache.AddBlessings().
// - ValidatorDataHash (and the wire form of the ValidataData)  with ValidationCache.AddValidatorData().
//
// The receiver looks up:
// - BlessingsHash with ValidationCache.LookupBlessingsData()
// - ValidatorDataHash with ValidationCache.LookupValidatorData()
//
// If not yet there, the receiver inserts the valus into its ValidationCache with:
// - ValidationCache.AddWireBlessings()
// - ValidationCache.AddValidatorData()
type DataWithSignature struct {
	Data []Item
	// BlessingsHash is a key for the validation cache; the corresponding
	// cached value is a security.Blessings.
	BlessingsHash []byte
	// AuthorSigned is the signature of Data and BlessingsHash using the
	// private key associated with the blessings hashed in BlessingsHash.
	AuthorSigned security.Signature
	IsValidated  bool // Whether fields below are meaningful.
	// ValidatorDataHash is a key for the validation cache returned by
	// ValidatorData.Hash(); the corresponding cached value is the
	// ValidatorData.
	ValidatorDataHash []byte
	ValidatorSigned   security.Signature
}

func (DataWithSignature) __VDLReflect(struct {
	Name string `vdl:"v.io/x/ref/services/syncbase/signing.DataWithSignature"`
}) {
}

func (x DataWithSignature) VDLIsZero() bool {
	if len(x.Data) != 0 {
		return false
	}
	if len(x.BlessingsHash) != 0 {
		return false
	}
	if !x.AuthorSigned.VDLIsZero() {
		return false
	}
	if x.IsValidated {
		return false
	}
	if len(x.ValidatorDataHash) != 0 {
		return false
	}
	if !x.ValidatorSigned.VDLIsZero() {
		return false
	}
	return true
}

func (x DataWithSignature) VDLWrite(enc vdl.Encoder) error {
	if err := enc.StartValue(__VDLType_struct_3); err != nil {
		return err
	}
	if len(x.Data) != 0 {
		if err := enc.NextField("Data"); err != nil {
			return err
		}
		if err := __VDLWriteAnon_list_1(enc, x.Data); err != nil {
			return err
		}
	}
	if len(x.BlessingsHash) != 0 {
		if err := enc.NextField("BlessingsHash"); err != nil {
			return err
		}
		if err := enc.StartValue(__VDLType_list_1); err != nil {
			return err
		}
		if err := enc.EncodeBytes(x.BlessingsHash); err != nil {
			return err
		}
		if err := enc.FinishValue(); err != nil {
			return err
		}
	}
	if !x.AuthorSigned.VDLIsZero() {
		if err := enc.NextField("AuthorSigned"); err != nil {
			return err
		}
		if err := x.AuthorSigned.VDLWrite(enc); err != nil {
			return err
		}
	}
	if x.IsValidated {
		if err := enc.NextField("IsValidated"); err != nil {
			return err
		}
		if err := enc.StartValue(vdl.BoolType); err != nil {
			return err
		}
		if err := enc.EncodeBool(x.IsValidated); err != nil {
			return err
		}
		if err := enc.FinishValue(); err != nil {
			return err
		}
	}
	if len(x.ValidatorDataHash) != 0 {
		if err := enc.NextField("ValidatorDataHash"); err != nil {
			return err
		}
		if err := enc.StartValue(__VDLType_list_1); err != nil {
			return err
		}
		if err := enc.EncodeBytes(x.ValidatorDataHash); err != nil {
			return err
		}
		if err := enc.FinishValue(); err != nil {
			return err
		}
	}
	if !x.ValidatorSigned.VDLIsZero() {
		if err := enc.NextField("ValidatorSigned"); err != nil {
			return err
		}
		if err := x.ValidatorSigned.VDLWrite(enc); err != nil {
			return err
		}
	}
	if err := enc.NextField(""); err != nil {
		return err
	}
	return enc.FinishValue()
}

func __VDLWriteAnon_list_1(enc vdl.Encoder, x []Item) error {
	if err := enc.StartValue(__VDLType_list_4); err != nil {
		return err
	}
	if err := enc.SetLenHint(len(x)); err != nil {
		return err
	}
	for i := 0; i < len(x); i++ {
		if err := enc.NextEntry(false); err != nil {
			return err
		}
		switch {
		case x[i] == nil:
			// Write the zero value of the union type.
			if err := vdl.ZeroValue(__VDLType_union_2).VDLWrite(enc); err != nil {
				return err
			}
		default:
			if err := x[i].VDLWrite(enc); err != nil {
				return err
			}
		}
	}
	if err := enc.NextEntry(true); err != nil {
		return err
	}
	return enc.FinishValue()
}

func (x *DataWithSignature) VDLRead(dec vdl.Decoder) error {
	*x = DataWithSignature{}
	if err := dec.StartValue(__VDLType_struct_3); err != nil {
		return err
	}
	for {
		f, err := dec.NextField()
		if err != nil {
			return err
		}
		switch f {
		case "":
			return dec.FinishValue()
		case "Data":
			if err := __VDLReadAnon_list_1(dec, &x.Data); err != nil {
				return err
			}
		case "BlessingsHash":
			if err := dec.ReadValueBytes(-1, &x.BlessingsHash); err != nil {
				return err
			}
		case "AuthorSigned":
			if err := x.AuthorSigned.VDLRead(dec); err != nil {
				return err
			}
		case "IsValidated":
			switch value, err := dec.ReadValueBool(); {
			case err != nil:
				return err
			default:
				x.IsValidated = value
			}
		case "ValidatorDataHash":
			if err := dec.ReadValueBytes(-1, &x.ValidatorDataHash); err != nil {
				return err
			}
		case "ValidatorSigned":
			if err := x.ValidatorSigned.VDLRead(dec); err != nil {
				return err
			}
		default:
			if err := dec.SkipValue(); err != nil {
				return err
			}
		}
	}
}

func __VDLReadAnon_list_1(dec vdl.Decoder, x *[]Item) error {
	if err := dec.StartValue(__VDLType_list_4); err != nil {
		return err
	}
	if len := dec.LenHint(); len > 0 {
		*x = make([]Item, 0, len)
	} else {
		*x = nil
	}
	for {
		switch done, err := dec.NextEntry(); {
		case err != nil:
			return err
		case done:
			return dec.FinishValue()
		default:
			var elem Item
			if err := VDLReadItem(dec, &elem); err != nil {
				return err
			}
			*x = append(*x, elem)
		}
	}
}

// WireValidatorData is the wire form of ValidatorData.
// It excludes the unmarshalled form of the public key.
type WireValidatorData struct {
	Names               []string // Names of valid signing blessings in the Blessings referred to by BlessingsHash.
	MarshalledPublicKey []byte   // PublicKey, marshalled with MarshalBinary().
}

func (WireValidatorData) __VDLReflect(struct {
	Name string `vdl:"v.io/x/ref/services/syncbase/signing.WireValidatorData"`
}) {
}

func (x WireValidatorData) VDLIsZero() bool {
	if len(x.Names) != 0 {
		return false
	}
	if len(x.MarshalledPublicKey) != 0 {
		return false
	}
	return true
}

func (x WireValidatorData) VDLWrite(enc vdl.Encoder) error {
	if err := enc.StartValue(__VDLType_struct_6); err != nil {
		return err
	}
	if len(x.Names) != 0 {
		if err := enc.NextField("Names"); err != nil {
			return err
		}
		if err := __VDLWriteAnon_list_2(enc, x.Names); err != nil {
			return err
		}
	}
	if len(x.MarshalledPublicKey) != 0 {
		if err := enc.NextField("MarshalledPublicKey"); err != nil {
			return err
		}
		if err := enc.StartValue(__VDLType_list_1); err != nil {
			return err
		}
		if err := enc.EncodeBytes(x.MarshalledPublicKey); err != nil {
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

func __VDLWriteAnon_list_2(enc vdl.Encoder, x []string) error {
	if err := enc.StartValue(__VDLType_list_7); err != nil {
		return err
	}
	if err := enc.SetLenHint(len(x)); err != nil {
		return err
	}
	for i := 0; i < len(x); i++ {
		if err := enc.NextEntry(false); err != nil {
			return err
		}
		if err := enc.StartValue(vdl.StringType); err != nil {
			return err
		}
		if err := enc.EncodeString(x[i]); err != nil {
			return err
		}
		if err := enc.FinishValue(); err != nil {
			return err
		}
	}
	if err := enc.NextEntry(true); err != nil {
		return err
	}
	return enc.FinishValue()
}

func (x *WireValidatorData) VDLRead(dec vdl.Decoder) error {
	*x = WireValidatorData{}
	if err := dec.StartValue(__VDLType_struct_6); err != nil {
		return err
	}
	for {
		f, err := dec.NextField()
		if err != nil {
			return err
		}
		switch f {
		case "":
			return dec.FinishValue()
		case "Names":
			if err := __VDLReadAnon_list_2(dec, &x.Names); err != nil {
				return err
			}
		case "MarshalledPublicKey":
			if err := dec.ReadValueBytes(-1, &x.MarshalledPublicKey); err != nil {
				return err
			}
		default:
			if err := dec.SkipValue(); err != nil {
				return err
			}
		}
	}
}

func __VDLReadAnon_list_2(dec vdl.Decoder, x *[]string) error {
	if err := dec.StartValue(__VDLType_list_7); err != nil {
		return err
	}
	if len := dec.LenHint(); len > 0 {
		*x = make([]string, 0, len)
	} else {
		*x = nil
	}
	for {
		switch done, elem, err := dec.NextEntryValueString(); {
		case err != nil:
			return err
		case done:
			return dec.FinishValue()
		default:
			*x = append(*x, elem)
		}
	}
}

// Hold type definitions in package-level variables, for better performance.
var (
	__VDLType_list_1   *vdl.Type
	__VDLType_union_2  *vdl.Type
	__VDLType_struct_3 *vdl.Type
	__VDLType_list_4   *vdl.Type
	__VDLType_struct_5 *vdl.Type
	__VDLType_struct_6 *vdl.Type
	__VDLType_list_7   *vdl.Type
)

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
	vdl.Register((*Item)(nil))
	vdl.Register((*DataWithSignature)(nil))
	vdl.Register((*WireValidatorData)(nil))

	// Initialize type definitions.
	__VDLType_list_1 = vdl.TypeOf((*[]byte)(nil))
	__VDLType_union_2 = vdl.TypeOf((*Item)(nil))
	__VDLType_struct_3 = vdl.TypeOf((*DataWithSignature)(nil)).Elem()
	__VDLType_list_4 = vdl.TypeOf((*[]Item)(nil))
	__VDLType_struct_5 = vdl.TypeOf((*security.Signature)(nil)).Elem()
	__VDLType_struct_6 = vdl.TypeOf((*WireValidatorData)(nil)).Elem()
	__VDLType_list_7 = vdl.TypeOf((*[]string)(nil))

	return struct{}{}
}
