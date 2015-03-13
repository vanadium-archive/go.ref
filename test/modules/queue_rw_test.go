package modules_test

import (
	"bytes"
	"io"
	"testing"

	"v.io/x/ref/test/modules"
	"v.io/x/ref/test/testutil"
)

func TestQueueRW(t *testing.T) {
	q := modules.NewRW()
	size := testutil.Intn(1000)
	data := testutil.RandomBytes(size)
	begin := 0
	for {
		end := begin + testutil.Intn(100) + 1
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
	if err := q.Close(); err != nil {
		t.Fatalf("err %v", err)
	}
	readData := make([]byte, 0, size)
	for {
		buf := make([]byte, testutil.Intn(100)+1)
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
