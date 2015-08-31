// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package clock

import (
	"bytes"
	"encoding/binary"
	"syscall"
	"time"
	"unsafe"
)

// This file contains darwin specific implementations of functions for clock
// package.

// ElapsedTime returns the time elapsed since last boot.
// Darwin provides a system call "kern.boottime" which returns a Timeval32
// object containing the boot time for the system. Darwin calculates this
// boottime based on the current clock and the internal tracking of elapsed
// time since boot. Hence if the clock is changed, the boot time changes along
// with it. So the difference between the current time and boot time will always
// give us the correct elapsed time since boot.
func (sc *systemClockImpl) ElapsedTime() (time.Duration, error) {
	tv := syscall.Timeval32{}

	if err := sysctlbyname("kern.boottime", &tv); err != nil {
		return 0, err
	}
	return time.Since(time.Unix(int64(tv.Sec), int64(tv.Usec)*1000)), nil
}

// Generic Sysctl buffer unmarshalling.
func sysctlbyname(name string, data interface{}) (err error) {
	val, err := syscall.Sysctl(name)
	if err != nil {
		return err
	}

	buf := []byte(val)

	switch v := data.(type) {
	case *uint64:
		*v = *(*uint64)(unsafe.Pointer(&buf[0]))
		return
	}

	bbuf := bytes.NewBuffer([]byte(val))
	return binary.Read(bbuf, binary.LittleEndian, data)
}
