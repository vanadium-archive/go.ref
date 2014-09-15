package serialization

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"veyron2/security"
	"veyron2/vom"
)

// verifyingReader implements io.Reader.
type verifyingReader struct {
	data io.Reader

	chunkSizeBytes int
	curChunk       bytes.Buffer
	hashes         bytes.Buffer
}

func (r *verifyingReader) Read(p []byte) (int, error) {
	bytesRead := 0
	for len(p) > 0 {
		if err := r.readChunk(); err != nil {
			return bytesRead, err
		}

		n, err := r.curChunk.Read(p)
		bytesRead = bytesRead + n
		if err != nil {
			return bytesRead, err
		}

		p = p[n:]
	}
	return bytesRead, nil
}

// NewVerifyingReader returns an io.Reader that ensures that all data returned
// by Read calls was written using a NewSigningWriter (by a principal possessing
// a signer corresponding to the provided public key), and has not been modified
// since (ensuring integrity and authenticity of data).
func NewVerifyingReader(data, signature io.Reader, key security.PublicKey) (io.Reader, error) {
	if (data == nil) || (signature == nil) || (key == nil) {
		return nil, fmt.Errorf("data:%v signature:%v key:%v cannot be nil", data, signature, key)
	}
	r := &verifyingReader{data: data}
	if err := r.verifySignature(signature, key); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *verifyingReader) readChunk() error {
	if r.curChunk.Len() > 0 {
		return nil
	}
	hash := make([]byte, sha256.Size)
	if _, err := r.hashes.Read(hash); err == io.EOF {
		return nil
	} else if err != nil {
		return err
	}

	if _, err := io.CopyN(&r.curChunk, r.data, int64(r.chunkSizeBytes)); err != nil && err != io.EOF {
		return err
	}

	if wantHash := sha256.Sum256(r.curChunk.Bytes()); !bytes.Equal(hash, wantHash[:]) {
		return errors.New("data has been modified since being written")
	}
	return nil
}

func (r *verifyingReader) verifySignature(signature io.Reader, key security.PublicKey) error {
	dec := vom.NewDecoder(signature)
	signatureHash := sha256.New()

	var h header
	if err := dec.Decode(&h); err != nil {
		return fmt.Errorf("failed to decode header: %v", err)
	}
	r.chunkSizeBytes = h.ChunkSizeBytes
	if err := binary.Write(signatureHash, binary.LittleEndian, int64(r.chunkSizeBytes)); err != nil {
		return err
	}

	var signatureFound bool
	for !signatureFound {
		var i interface{}
		if err := dec.Decode(&i); err == io.EOF {
			break
		} else if err != nil {
			return err
		}

		switch v := i.(type) {
		case [sha256.Size]byte:
			if _, err := io.MultiWriter(&r.hashes, signatureHash).Write(v[:]); err != nil {
				return err
			}
		case security.Signature:
			signatureFound = true
			if !v.Verify(key, signatureHash.Sum(nil)) {
				return errors.New("signature verification failed")
			}
		default:
			return fmt.Errorf("invalid data of type: %T read from signature Reader", i)
		}
	}
	// Verify that no more data can be read from the signature Reader.
	if _, err := signature.Read(make([]byte, 1)); err != io.EOF {
		return fmt.Errorf("unexpected data found after signature")
	}
	return nil
}

func init() {
	vom.Register([sha256.Size]byte{})
}
