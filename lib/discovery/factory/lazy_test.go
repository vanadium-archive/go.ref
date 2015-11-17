// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package factory

import (
	"errors"
	"fmt"
	"testing"

	"v.io/v23/context"
	"v.io/v23/discovery"
	"v.io/v23/security"
)

type mock struct {
	numInits, numAdvertises, numScans, numCloses int
	initErr                                      error
}

func (m *mock) init() (discovery.T, error) {
	m.numInits++
	if m.initErr != nil {
		return nil, m.initErr
	}
	return m, nil
}

func (m *mock) Advertise(_ *context.T, _ *discovery.Service, _ []security.BlessingPattern) (<-chan struct{}, error) {
	m.numAdvertises++
	return nil, nil
}

func (m *mock) Scan(_ *context.T, _ string) (<-chan discovery.Update, error) {
	m.numScans++
	return nil, nil
}

func (m *mock) Close() {
	m.numCloses++
}

func (m *mock) check(numInits, numAdvertises, numScans, numCloses int) error {
	switch {
	case m.numInits != numInits:
		return fmt.Errorf("expect %d init calls, but got %d times", numInits, m.numInits)
	case m.numAdvertises != numAdvertises:
		return fmt.Errorf("expect %d advertise calls, but got %d times", numAdvertises, m.numAdvertises)
	case m.numScans != numScans:
		return fmt.Errorf("expect %d scan calls, but got %d times", numScans, m.numScans)
	case m.numCloses != numCloses:
		return fmt.Errorf("expect %d close calls, but got %d times", numCloses, m.numCloses)
	}
	return nil
}

func TestLazyFactory(t *testing.T) {
	m := mock{}
	d := newLazyFactory(m.init)

	if err := m.check(0, 0, 0, 0); err != nil {
		t.Error(err)
	}

	for i := 0; i < 3; i++ {
		d.Advertise(nil, nil, nil)
		if err := m.check(1, i+1, i, i); err != nil {
			t.Error(err)
		}

		d.Scan(nil, "")
		if err := m.check(1, i+1, i+1, i); err != nil {
			t.Error(err)
		}

		d.Close()
		if err := m.check(1, i+1, i+1, i+1); err != nil {
			t.Error(err)
		}
	}
}

func TestLazyFactoryClosed(t *testing.T) {
	m := mock{}
	d := newLazyFactory(m.init)

	d.Close()
	if err := m.check(0, 0, 0, 0); err != nil {
		t.Error(err)
	}

	// Closed already; Shouldn't initialize it again.
	if _, err := d.Advertise(nil, nil, nil); err != errClosed {
		t.Errorf("expected an error %v, but got %v", errClosed, err)
	}
	if err := m.check(0, 0, 0, 0); err != nil {
		t.Error(err)
	}

	if _, err := d.Scan(nil, ""); err != errClosed {
		t.Errorf("expected an error %v, but got %v", errClosed, err)
	}
	if err := m.check(0, 0, 0, 0); err != nil {
		t.Error(err)
	}
}

func TestLazyFactoryInitError(t *testing.T) {
	errInit := errors.New("test error")
	m := mock{initErr: errInit}
	d := newLazyFactory(m.init)

	if _, err := d.Advertise(nil, nil, nil); err != errInit {
		t.Errorf("expected an error %v, but got %v", errInit, err)
	}
	if err := m.check(1, 0, 0, 0); err != nil {
		t.Error(err)
	}

	if _, err := d.Scan(nil, ""); err != errInit {
		t.Errorf("expected an error %v, but got %v", errInit, err)
	}
	if err := m.check(1, 0, 0, 0); err != nil {
		t.Error(err)
	}

	d.Close()
	if err := m.check(1, 0, 0, 0); err != nil {
		t.Error(err)
	}
}
