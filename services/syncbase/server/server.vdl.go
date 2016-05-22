// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// This file was auto-generated by the vanadium vdl tool.
// Package: server

package server

import (
	"v.io/v23/security/access"
	"v.io/v23/services/syncbase"
	"v.io/v23/vdl"
)

var _ = __VDLInit() // Must be first; see __VDLInit comments for details.

//////////////////////////////////////////////////
// Type definitions

// ServiceData represents the persistent state of a Service.
type ServiceData struct {
	Version uint64 // covers the fields below
	Perms   access.Permissions
}

func (ServiceData) __VDLReflect(struct {
	Name string `vdl:"v.io/x/ref/services/syncbase/server.ServiceData"`
}) {
}

func (x ServiceData) VDLIsZero() bool {
	if x.Version != 0 {
		return false
	}
	if len(x.Perms) != 0 {
		return false
	}
	return true
}

func (x ServiceData) VDLWrite(enc vdl.Encoder) error {
	if err := enc.StartValue(__VDLType_struct_1); err != nil {
		return err
	}
	if x.Version != 0 {
		if err := enc.NextField("Version"); err != nil {
			return err
		}
		if err := enc.StartValue(vdl.Uint64Type); err != nil {
			return err
		}
		if err := enc.EncodeUint(x.Version); err != nil {
			return err
		}
		if err := enc.FinishValue(); err != nil {
			return err
		}
	}
	if len(x.Perms) != 0 {
		if err := enc.NextField("Perms"); err != nil {
			return err
		}
		if err := x.Perms.VDLWrite(enc); err != nil {
			return err
		}
	}
	if err := enc.NextField(""); err != nil {
		return err
	}
	return enc.FinishValue()
}

func (x *ServiceData) VDLRead(dec vdl.Decoder) error {
	*x = ServiceData{}
	if err := dec.StartValue(__VDLType_struct_1); err != nil {
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
		case "Version":
			switch value, err := dec.ReadValueUint(64); {
			case err != nil:
				return err
			default:
				x.Version = value
			}
		case "Perms":
			if err := x.Perms.VDLRead(dec); err != nil {
				return err
			}
		default:
			if err := dec.SkipValue(); err != nil {
				return err
			}
		}
	}
}

// DbInfo contains information about a single Database, stored in the
// service-level storage engine.
type DbInfo struct {
	Id syncbase.Id
	// Select fields from DatabaseOptions, needed in order to open storage engine
	// on restart.
	RootDir string // interpreted by storage engine
	Engine  string // name of storage engine, e.g. "leveldb"
}

func (DbInfo) __VDLReflect(struct {
	Name string `vdl:"v.io/x/ref/services/syncbase/server.DbInfo"`
}) {
}

func (x DbInfo) VDLIsZero() bool {
	return x == DbInfo{}
}

func (x DbInfo) VDLWrite(enc vdl.Encoder) error {
	if err := enc.StartValue(__VDLType_struct_3); err != nil {
		return err
	}
	if x.Id != (syncbase.Id{}) {
		if err := enc.NextField("Id"); err != nil {
			return err
		}
		if err := x.Id.VDLWrite(enc); err != nil {
			return err
		}
	}
	if x.RootDir != "" {
		if err := enc.NextField("RootDir"); err != nil {
			return err
		}
		if err := enc.StartValue(vdl.StringType); err != nil {
			return err
		}
		if err := enc.EncodeString(x.RootDir); err != nil {
			return err
		}
		if err := enc.FinishValue(); err != nil {
			return err
		}
	}
	if x.Engine != "" {
		if err := enc.NextField("Engine"); err != nil {
			return err
		}
		if err := enc.StartValue(vdl.StringType); err != nil {
			return err
		}
		if err := enc.EncodeString(x.Engine); err != nil {
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

func (x *DbInfo) VDLRead(dec vdl.Decoder) error {
	*x = DbInfo{}
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
		case "Id":
			if err := x.Id.VDLRead(dec); err != nil {
				return err
			}
		case "RootDir":
			switch value, err := dec.ReadValueString(); {
			case err != nil:
				return err
			default:
				x.RootDir = value
			}
		case "Engine":
			switch value, err := dec.ReadValueString(); {
			case err != nil:
				return err
			default:
				x.Engine = value
			}
		default:
			if err := dec.SkipValue(); err != nil {
				return err
			}
		}
	}
}

// DatabaseData represents the persistent state of a Database, stored in the
// per-database storage engine.
type DatabaseData struct {
	Id             syncbase.Id
	Version        uint64 // covers the Perms field below
	Perms          access.Permissions
	SchemaMetadata *syncbase.SchemaMetadata
}

func (DatabaseData) __VDLReflect(struct {
	Name string `vdl:"v.io/x/ref/services/syncbase/server.DatabaseData"`
}) {
}

func (x DatabaseData) VDLIsZero() bool {
	if x.Id != (syncbase.Id{}) {
		return false
	}
	if x.Version != 0 {
		return false
	}
	if len(x.Perms) != 0 {
		return false
	}
	if x.SchemaMetadata != nil {
		return false
	}
	return true
}

func (x DatabaseData) VDLWrite(enc vdl.Encoder) error {
	if err := enc.StartValue(__VDLType_struct_5); err != nil {
		return err
	}
	if x.Id != (syncbase.Id{}) {
		if err := enc.NextField("Id"); err != nil {
			return err
		}
		if err := x.Id.VDLWrite(enc); err != nil {
			return err
		}
	}
	if x.Version != 0 {
		if err := enc.NextField("Version"); err != nil {
			return err
		}
		if err := enc.StartValue(vdl.Uint64Type); err != nil {
			return err
		}
		if err := enc.EncodeUint(x.Version); err != nil {
			return err
		}
		if err := enc.FinishValue(); err != nil {
			return err
		}
	}
	if len(x.Perms) != 0 {
		if err := enc.NextField("Perms"); err != nil {
			return err
		}
		if err := x.Perms.VDLWrite(enc); err != nil {
			return err
		}
	}
	if x.SchemaMetadata != nil {
		if err := enc.NextField("SchemaMetadata"); err != nil {
			return err
		}
		enc.SetNextStartValueIsOptional()

		if err := x.SchemaMetadata.VDLWrite(enc); err != nil {
			return err
		}

	}
	if err := enc.NextField(""); err != nil {
		return err
	}
	return enc.FinishValue()
}

func (x *DatabaseData) VDLRead(dec vdl.Decoder) error {
	*x = DatabaseData{}
	if err := dec.StartValue(__VDLType_struct_5); err != nil {
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
		case "Id":
			if err := x.Id.VDLRead(dec); err != nil {
				return err
			}
		case "Version":
			switch value, err := dec.ReadValueUint(64); {
			case err != nil:
				return err
			default:
				x.Version = value
			}
		case "Perms":
			if err := x.Perms.VDLRead(dec); err != nil {
				return err
			}
		case "SchemaMetadata":
			if err := dec.StartValue(__VDLType_optional_6); err != nil {
				return err
			}
			if dec.IsNil() {
				x.SchemaMetadata = nil
				if err := dec.FinishValue(); err != nil {
					return err
				}
			} else {
				x.SchemaMetadata = new(syncbase.SchemaMetadata)
				dec.IgnoreNextStartValue()
				if err := x.SchemaMetadata.VDLRead(dec); err != nil {
					return err
				}
			}
		default:
			if err := dec.SkipValue(); err != nil {
				return err
			}
		}
	}
}

// Hold type definitions in package-level variables, for better performance.
var (
	__VDLType_struct_1   *vdl.Type
	__VDLType_map_2      *vdl.Type
	__VDLType_struct_3   *vdl.Type
	__VDLType_struct_4   *vdl.Type
	__VDLType_struct_5   *vdl.Type
	__VDLType_optional_6 *vdl.Type
	__VDLType_struct_7   *vdl.Type
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
	vdl.Register((*ServiceData)(nil))
	vdl.Register((*DbInfo)(nil))
	vdl.Register((*DatabaseData)(nil))

	// Initialize type definitions.
	__VDLType_struct_1 = vdl.TypeOf((*ServiceData)(nil)).Elem()
	__VDLType_map_2 = vdl.TypeOf((*access.Permissions)(nil))
	__VDLType_struct_3 = vdl.TypeOf((*DbInfo)(nil)).Elem()
	__VDLType_struct_4 = vdl.TypeOf((*syncbase.Id)(nil)).Elem()
	__VDLType_struct_5 = vdl.TypeOf((*DatabaseData)(nil)).Elem()
	__VDLType_optional_6 = vdl.TypeOf((*syncbase.SchemaMetadata)(nil))
	__VDLType_struct_7 = vdl.TypeOf((*syncbase.SchemaMetadata)(nil)).Elem()

	return struct{}{}
}
