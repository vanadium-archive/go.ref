package modules_test

import (
	"bytes"
	"io"
	"testing"

	"veyron.io/veyron/veyron/lib/modules"
	"veyron.io/veyron/veyron/lib/testutil"
)

func init() {
	if !modules.IsModulesProcess() {
		testutil.Init()
	}
}
func TestQueueRW(t *testing.T) {
	q := modules.NewRW()
	size := testutil.Rand.Intn(1000)
	data := testutil.RandomBytes(size)
	begin := 0
	for {
		end := begin + testutil.Rand.Intn(100) + 1
		if end > len(data) {
			end = len(data)
		}
		n, err := q.Write(data[begin:end])
		if err != nil {
			t.Fatalf("Write failed: %v", err)
		}
		begin = begin + n
		if begin == len(data) {
			break
		}
	}
	// This marks EOF.
	if _, err := q.Write([]byte{}); err != nil {
		t.Fatalf("err %v", err)
	}
	readData := make([]byte, 0, size)
	for {
		buf := make([]byte, testutil.Rand.Intn(100)+1)
		n, err := q.Read(buf)
		if n > 0 {
			readData = append(readData, buf[:n]...)
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("Read failed: %v", err)
		}
	}
	if size != len(readData) {
		t.Fatalf("Mismatching data size: %d != %d", size, len(readData))
	}
	if !bytes.Equal(data, readData) {
		t.Fatalf("Diffing data:\n%v\n%v", data, readData)
	}
}
