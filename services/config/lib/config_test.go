package config

import (
	"errors"
	"fmt"
	"io/ioutil"
	"math/rand"
	"os"
	"testing"
	"time"
)

// A config file to be written to disk.
var configFileA = `
the:rain
 in:spain
falls: mainly
 on : the 
# something ugly this way comes
plain:	today
version : 1
`

// A map referring to the config in configFileA.
var referenceA = map[string]string{
	"the":     "rain",
	"in":      "spain",
	"falls":   "mainly",
	"on":      "the",
	"plain":   "today",
	"version": "1",
}

// Another config file to be written to disk.
var configFileB = `
the:rain
 in:spain
falls: mainly
 on : my foot 
# something ugly this way comes
version : 1.1
`

// A map referring to the config in configFileB.
var referenceB = map[string]string{
	"the":     "rain",
	"in":      "spain",
	"falls":   "mainly",
	"on":      "my foot",
	"version": "1.1",
}

func compare(actual map[string]string, reference map[string]string) error {
	for k, rv := range reference {
		av, ok := actual[k]
		if !ok {
			return fmt.Errorf("missing entry for key %q", k)
		}
		if av != rv {
			return fmt.Errorf("bad value for key %q: expected %q, got %q", k, rv, av)
		}
	}
	for k, av := range actual {
		if _, ok := reference[k]; !ok {
			return fmt.Errorf("unexpected pair %q : %q", k, av)
		}
	}
	return nil
}

func compareConfigToReference(cs ConfigService, ref map[string]string) error {
	actual, err := cs.GetAll()
	if err != nil {
		return err
	}
	return compare(actual, ref)
}

func compareFileToReference(file string, ref map[string]string) error {
	c, err := readFile(file)
	if err != nil {
		return err
	}
	return compare(c.pairs, ref)
}

func waitForConsistency(cs ConfigService, ref map[string]string) error {
	finalerr := errors.New("inconsistent")
	for loops := 0; loops < 30; loops++ {
		actual, err := cs.GetAll()
		if err == nil {
			err := compare(actual, ref)
			if err == nil {
				return nil
			}
			finalerr = err
		}
		time.Sleep(500 * time.Millisecond)
	}
	return errors.New("timeout waiting for consistency: " + finalerr.Error())
}

func pickRandomPort() uint16 {
	rand.Seed(time.Now().UnixNano())
	return uint16(rand.Intn(1000)) + 5354
}

func testConfig(fileA, fileB, fileD string) error {
	port := pickRandomPort()

	// Write a config to fileA.
	if err := ioutil.WriteFile(fileA, []byte(configFileA), 0644); err != nil {
		return fmt.Errorf("writing initial %s: %s", fileA, err)
	}

	// Start a config service with a file and compare the parsed file to the reference.
	csa, err := MDNSConfigService("foo", fileA, true, port)
	if err != nil {
		return fmt.Errorf("creating service %s", err)
	}
	defer csa.Stop()
	if err := compareConfigToReference(csa, referenceA); err != nil {
		return fmt.Errorf("confA ~ refA: %s", err)
	}

	// Create a second instance with a non existant file.
	csb, err := MDNSConfigService("foo", fileB, true, port)
	if err != nil {
		return fmt.Errorf("creating service %s", err)
	}
	defer csb.Stop()

	// Create a third passive instance with no file.
	csc, err := MDNSConfigService("foo", "", true, port)
	if err != nil {
		return fmt.Errorf("creating service %s", err)
	}
	defer csc.Stop()

	// Loop till the second instance gets a config or we time out.
	if err := waitForConsistency(csb, referenceA); err != nil {
		return fmt.Errorf("confB ~ refA: %s", err)
	}

	// Make sure that the new instance updated its file.
	if err := compareFileToReference(fileB, referenceA); err != nil {
		return fmt.Errorf("fileB ~ refA: %s", err)
	}

	// Rewrite/Reread the second instance's file, make sure the it rereads it correctly.
	if err := ioutil.WriteFile(fileB, []byte(configFileB), 0644); err != nil {
		return fmt.Errorf("writing fileB: %s", err)
	}
	if err := csb.Reread(); err != nil {
		return fmt.Errorf("Rereading fileB: %s", err)
	}
	if err := compareConfigToReference(csb, referenceB); err != nil {
		return fmt.Errorf("confB ~ refB: %s", err)
	}
	// Loop till the first instance changes its config or we time out.
	if err := waitForConsistency(csa, referenceB); err != nil {
		return fmt.Errorf("confA ~ refB: %s", err)
	}

	// Make sure that the first instance updated its file.
	if err := compareFileToReference(fileA, referenceB); err != nil {
		return fmt.Errorf("fileA ~ refB: %s", err)
	}

	// Loop till the passive instance changes its config or we time out.
	if err := waitForConsistency(csc, referenceB); err != nil {
		return fmt.Errorf("confC ~ refB: %s", err)
	}

	// Create an instance with an lower numbered version.  Make sure it doesn't
	// overcome the higher numbered one.
	if err := ioutil.WriteFile(fileD, []byte(configFileA), 0644); err != nil {
		return fmt.Errorf("writing initial %s: %s", fileD, err)
	}
	csd, err := MDNSConfigService("foo", fileD, true, port)
	if err != nil {
		return fmt.Errorf("creating service %s", err)
	}
	defer csd.Stop()
	if err := waitForConsistency(csd, referenceB); err != nil {
		return fmt.Errorf("confD ~ refB: %s", err)
	}
	if err := waitForConsistency(csc, referenceA); err == nil {
		return errors.New("eventual consistency picked wrong version for C")
	}

	return nil
}

func createTempFile(t *testing.T, base string) string {
	f, err := ioutil.TempFile("", base)
	if err != nil {
		t.Fatal("creating temp file: %s", err)
	}
	f.Close()
	return f.Name()
}

func TestConfig(t *testing.T) {
	// Create temporary files.
	fileA := createTempFile(t, "testconfigA")
	defer os.Remove(fileA)
	fileB := createTempFile(t, "testconfigB")
	defer os.Remove(fileB)
	fileD := createTempFile(t, "testconfigD")
	defer os.Remove(fileD)

	if err := testConfig(fileA, fileB, fileD); err != nil {
		t.Fatal(err)
	}
}

func TestConfigStream(t *testing.T) {
	// Create temporary file.
	fileA := createTempFile(t, "testconfigA")
	defer os.Remove(fileA)

	// Start a cs service.
	cs, err := MDNSConfigService("foo", fileA, true, pickRandomPort())
	if err != nil {
		t.Fatal("creating service %s", err)
	}

	// Watch a single key and all keys.
	ws := cs.Watch("version")
	wa := cs.WatchAll()

	// Since there's no config yet, we should just get a
	// deleted entry for the single key.
	select {
	case p := <-ws:
		if !p.Nonexistant {
			t.Fatal("expected Nonexistant: got %v", p)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for single config var")
	}

	// Write a config to the file and read it.
	if err := ioutil.WriteFile(fileA, []byte(configFileA), 0644); err != nil {
		t.Fatal("writing initial %s: %s", fileA, err)
	}
	if err := cs.Reread(); err != nil {
		t.Fatal("rereading config: %s", err)
	}

	// We should now have a version number.
	select {
	case p := <-ws:
		if p.Nonexistant || p.Key != "version" || p.Value != "1" {
			t.Fatal("expected [version, 1, false]: got %v", p)
		}

	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for single config var")
	}

	// And we should be streamed the whole version.
	cmap := make(map[string]string, 0)
L:
	for {
		select {
		case p := <-wa:
			if p.Nonexistant {
				t.Fatal("unexpected %v", p)
			}
			cmap[p.Key] = p.Value
			err := compare(referenceA, cmap)
			if err == nil {
				break L
			}
		case <-time.After(2 * time.Second):
			t.Fatal("timeout waiting for all config vars")
		}
	}

	// Change and reread the config file.
	if err := ioutil.WriteFile(fileA, []byte(configFileB), 0644); err != nil {
		t.Fatal("writing initial %s: %s", fileA, err)
	}
	if err := cs.Reread(); err != nil {
		t.Fatal("rereading config: %s", err)
	}

	// The version should have changed.
	select {
	case p := <-ws:
		if p.Nonexistant || p.Key != "version" || p.Value != "1.1" {
			t.Fatal("expected [version, 1.1, false]: got %v", p)
		}

	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for single config var")
	}

	// And we should now be consistent with referenceB
L2:
	for {
		select {
		case p := <-wa:
			if p.Nonexistant {
				delete(cmap, p.Key)
				break
			}
			cmap[p.Key] = p.Value
			err := compare(referenceB, cmap)
			if err == nil {
				break L2
			}
		case <-time.After(2 * time.Second):
			t.Fatal("timeout waiting for all config vars")
		}
	}
}
