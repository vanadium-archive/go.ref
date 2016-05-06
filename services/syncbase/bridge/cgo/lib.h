// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

#ifndef V23_SYNCBASE_LIB_H_
#define V23_SYNCBASE_LIB_H_

#include <stdbool.h>
#include <stdint.h>

// TODO(sadovsky): Add types and functions for watch and sync.

////////////////////////////////////////
// Generic types

// string
typedef struct {
  char* p;
  int n;
} v23_syncbase_String;

// []byte
typedef struct {
  uint8_t* p;
  int n;
} v23_syncbase_Bytes;

// []string
typedef struct {
  v23_syncbase_String* p;
  int n;
} v23_syncbase_Strings;

////////////////////////////////////////
// Vanadium-specific types

// verror.E
typedef struct {
  v23_syncbase_String id;
  unsigned int actionCode;
  v23_syncbase_String msg;
  v23_syncbase_String stack;
} v23_syncbase_VError;

// access.Permissions
// TODO(sadovsky): Decide how to represent perms.
typedef struct {
  v23_syncbase_String json;
} v23_syncbase_Permissions;

////////////////////////////////////////
// Syncbase-specific types

// syncbase.Id
typedef struct {
  v23_syncbase_String blessing;
  v23_syncbase_String name;
} v23_syncbase_Id;

// []syncbase.Id
typedef struct {
  v23_syncbase_Id* p;
  int n;
} v23_syncbase_Ids;

// syncbase.BatchOptions
typedef struct {
  v23_syncbase_String hint;
  bool readOnly;
} v23_syncbase_BatchOptions;

// syncbase.KeyValue
typedef struct {
  v23_syncbase_String key;
  v23_syncbase_Bytes value;
} v23_syncbase_KeyValue;

// syncbase.SyncgroupSpec
typedef struct {
  v23_syncbase_String description;
  v23_syncbase_Permissions perms;
  v23_syncbase_Ids collections;
  v23_syncbase_Strings mountTables;
  bool isPrivate;
} v23_syncbase_SyncgroupSpec;

// syncbase.SyncgroupMemberInfo
typedef struct {
  uint8_t syncPriority;
  uint8_t blobDevType;
} v23_syncbase_SyncgroupMemberInfo;

// map[string]syncbase.SyncgroupMemberInfo
typedef struct {
  v23_syncbase_String* keys;
  v23_syncbase_SyncgroupMemberInfo* values;
  int n;
} v23_syncbase_SyncgroupMemberInfoMap;

////////////////////////////////////////
// Functions

// Callbacks are represented as struct
// {v23_syncbase_Handle, f(v23_syncbase_Handle, ...)} to allow for currying
// RefMap handles to Swift closures.
// https://forums.developer.apple.com/message/15725#15725

typedef int v23_syncbase_Handle;

typedef struct {
  v23_syncbase_Handle hOnKeyValue;
  v23_syncbase_Handle hOnDone;
  void (*onKeyValue)(v23_syncbase_Handle hOnKeyValue, v23_syncbase_KeyValue);
  void (*onDone)(v23_syncbase_Handle hOnKeyValue, v23_syncbase_Handle hOnDone, v23_syncbase_VError);
} v23_syncbase_CollectionScanCallbacks;

#endif  // V23_SYNCBASE_LIB_H_
