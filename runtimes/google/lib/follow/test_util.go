package follow

import (
	"errors"
	"fmt"
	"io"
	"os"
	"time"
)

var (
	errTimedOut               = errors.New("timed out")
	errCantAppend             = errors.New("cannot append string to file")
	errUnexpectedModification = errors.New("unexpected modification event")
)

// readString reads a string of the specified length from the reader.
// Returns an error if:
//  (A) reader.Read returned an error
//  (A) reader.Read timed out
func readString(reader io.Reader, length int, timeout time.Duration) (string, error) {
	p := make([]byte, length, length)

	c := make(chan string)
	e := make(chan error)
	go func() {
		n, err := reader.Read(p)
		if err != nil {
			e <- err
			return
		}
		c <- string(p[:n])
	}()

	timer := time.After(timeout)
	select {
	case err := <-e:
		return "", err
	case s := <-c:
		return s, nil
	case <-timer:
		return "", errTimedOut
	}
}

// writeString appends a string to a file, and flushes its new contents to
// stable storage.
func writeString(file *os.File, s string) error {
	n, err := io.WriteString(file, s)
	if err != nil {
		return errors.New(fmt.Sprintf("io.WriteString() failed: %v", err))
	}
	if n < len(s) {
		return errCantAppend
	}
	file.Sync()
	return nil
}

// expectSilence tests that no events are received on the events channel
// within the duration specified by timeout.
func expectSilence(events <-chan error, timeout time.Duration) error {
	timer := time.After(timeout)
	select {
	case err := <-events:
		if err != nil {
			return err
		}
		return errUnexpectedModification
	case <-timer:
		// all's well
		return nil
	}
}

// expectModification tests that a modification event is received on the events
// channel within the duration specified by timeout.
func expectModification(events <-chan error, timeout time.Duration) error {
	timer := time.After(timeout)
	select {
	case <-timer:
		return errTimedOut
	case err := <-events:
		if err != nil {
			return err
		}
		// all's well
		return nil
	}
}

// testModification tests that the watcher sends events when the file is
// modified.
func testModification(file *os.File, watcher *fsWatcher, timeout time.Duration) error {
	// no modifications, expect no events.
	if err := expectSilence(watcher.events, timeout); err != nil {
		return errors.New(fmt.Sprintf("expectSilence() failed with no modifications: %v ", err))
	}
	// modify once, expect event.
	if err := writeString(file, "modification one"); err != nil {
		return errors.New(fmt.Sprintf("writeString() failed on modification one: %v ", err))
	}
	if err := expectModification(watcher.events, timeout); err != nil {
		return errors.New(fmt.Sprintf("expectModication() failed on modification one: %v ", err))
	}
	// no further modifications, expect no events.
	if err := expectSilence(watcher.events, timeout); err != nil {
		return errors.New(fmt.Sprintf("expectSilence() failed after modification one: %v ", err))
	}
	// modify again, expect event.
	if err := writeString(file, "modification two"); err != nil {
		return errors.New(fmt.Sprintf("writeString() failed on modification two: %v ", err))
	}
	if err := expectModification(watcher.events, timeout); err != nil {
		return errors.New(fmt.Sprintf("expectModification() failed on modification two: %v ", err))
	}
	// no further modifications, expect no events.
	if err := expectSilence(watcher.events, timeout); err != nil {
		return errors.New(fmt.Sprintf("expectSilence() failed after modification two: %v ", err))
	}
	return nil
}

// testClose tests the implementation of fsReader.Read(). Specifically,
// tests that Read() blocks if the requested bytes are not available for
// reading in the underlying file.
func testReadPartial(testFileName string, watcher *fsWatcher, timeout time.Duration) error {
	s0 := "part"

	// Open the file for writing.
	testfileW, err := os.OpenFile(testFileName, os.O_WRONLY, 0)
	if err != nil {
		return errors.New(fmt.Sprintf("os.OpenFile() failed: %v", err))
	}
	defer testfileW.Close()

	// Open the file for reading.
	testfileR, err := os.Open(testFileName)
	if err != nil {
		return errors.New(fmt.Sprintf("os.Open() failed: %v", err))
	}
	// Create the reader.
	reader, err := newCustomReader(testfileR, watcher)
	if err != nil {
		return errors.New(fmt.Sprintf("newCustomReader() failed: %v", err))
	}
	defer reader.Close()

	// Write a part of the string.
	if err := writeString(testfileW, s0); err != nil {
		return errors.New(fmt.Sprintf("writeString() failed: %v ", err))
	}

	// Some bytes written, but not enough to fill the buffer. Read should
	// still succeed.
	s, err := readString(reader, len(s0)+1, timeout)
	if s != s0 {
		return errors.New(fmt.Sprintf("Expected to read: %v, but read: %v", s0, s))
	}
	// No more bytes written, so read should block.
	s, err = readString(reader, len(s0)+1, timeout)
	if err != errTimedOut {
		return errors.New(fmt.Sprintf("readString() failed, expected timeout: %v", err))
	}
	if s != "" {
		return errors.New(fmt.Sprintf("Did not expect to read: %v", s))
	}

	return nil
}

// testClose tests the implementation of fsReader.Read(). Specifically,
// tests that Read() returns the requested bytes when they are available
// for reading in the underlying file.
func testReadFull(testFileName string, watcher *fsWatcher, timeout time.Duration) error {
	s0, s1, s2 := "partitioned", "parti", "tioned"

	// Open the file for writing.
	testfileW, err := os.OpenFile(testFileName, os.O_WRONLY, 0)
	if err != nil {
		return errors.New(fmt.Sprintf("os.Open() failed: %v", err))
	}
	defer testfileW.Close()

	// Open the file for reading.
	testfileR, err := os.Open(testFileName)
	if err != nil {
		return errors.New(fmt.Sprintf("os.Open() failed: %v", err))
	}
	// Create the reader.
	reader, err := newCustomReader(testfileR, watcher)
	if err != nil {
		return errors.New(fmt.Sprintf("newCustomReader() failed: %v", err))
	}
	defer reader.Close()

	// Write part one of the string.
	if err := writeString(testfileW, s1); err != nil {
		return errors.New(fmt.Sprintf("writeString() failed on part one: %v ", err))
	}

	// Write part two of the string.
	if err := writeString(testfileW, s2); err != nil {
		return errors.New(fmt.Sprintf("writeString() failed on part two: %v ", err))
	}

	// Enough bytes written, so read should succeed.
	s, err := readString(reader, len(s0), timeout)
	if err != nil {
		return errors.New(fmt.Sprintf("readString() failed: %v", err))
	}
	if s != s0 {
		return errors.New(fmt.Sprintf("Expected to read: %v, but read: %v", s0, s))
	}

	return nil
}

// testClose tests the implementation of fsReader.Close()
func testClose(testFileName string, watcher *fsWatcher, timeout time.Duration) error {
	s := "word"

	// Open the file for writing.
	testfileW, err := os.OpenFile(testFileName, os.O_WRONLY, 0)
	if err != nil {
		return errors.New(fmt.Sprintf("os.OpenFile() failed: %v", err))
	}
	defer testfileW.Close()

	// Open the file for reading.
	testfileR, err := os.Open(testFileName)
	if err != nil {
		return errors.New(fmt.Sprintf("os.Open() failed: %v", err))
	}
	// Create the reader.
	reader, err := newCustomReader(testfileR, watcher)
	if err != nil {
		return errors.New(fmt.Sprintf("newCustomReader() failed: %v", err))
	}

	// Close the reader.
	if err := reader.Close(); err != nil {
		return errors.New(fmt.Sprintf("Close() failed: %v", err))
	}

	// Close the reader again.
	if err := reader.Close(); err != nil {
		return errors.New(fmt.Sprintf("Duplicate Close() failed: %v", err))
	}

	// Write the string.
	if err := writeString(testfileW, s); err != nil {
		return errors.New(fmt.Sprintf("writeString() failed: %v ", err))
	}

	// Reader is closed, readString() should fail.
	if _, err := readString(reader, len(s), timeout); err == nil {
		return errors.New("Expected readString() to fail")
	} else if err != io.EOF {
		return errors.New(fmt.Sprintf("readString() failed with unexpected error: %v", err))
	}

	return nil
}
