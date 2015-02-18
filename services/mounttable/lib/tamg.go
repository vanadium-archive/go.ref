package mounttable

import (
	"strconv"

	"v.io/core/veyron2/security"
	"v.io/core/veyron2/services/security/access"
	"v.io/core/veyron2/verror"
)

// TAMG associates a generation with a TaggedACLMap
type TAMG struct {
	tam        access.TaggedACLMap
	generation int32
}

func NewTAMG() *TAMG {
	return &TAMG{tam: make(access.TaggedACLMap)}
}

// Set sets the ACLs iff generation matches the current generation.  If the set happens, the generation is advanced.
// If b is nil, this creates a new TAMG.
func (b *TAMG) Set(genstr string, tam access.TaggedACLMap) (*TAMG, error) {
	if b == nil {
		b = new(TAMG)
	}
	if len(genstr) > 0 {
		gen, err := strconv.ParseInt(genstr, 10, 32)
		if err != nil {
			return b, verror.NewErrBadEtag(nil)
		}
		if gen >= 0 && int32(gen) != b.generation {
			return b, verror.NewErrBadEtag(nil)
		}
	}
	b.tam = tam
	b.generation++
	// Protect against wrap.
	if b.generation < 0 {
		b.generation = 0
	}
	return b, nil
}

// Get returns the current generation and acls.
func (b *TAMG) Get() (string, access.TaggedACLMap) {
	if b == nil {
		return "", nil
	}
	return strconv.FormatInt(int64(b.generation), 10), b.tam
}

// GetACLForTag returns the current acls for the given tag.
func (b *TAMG) GetACLForTag(tag string) (access.ACL, bool) {
	acl, exists := b.tam[tag]
	return acl, exists
}

// Copy copies the receiver.
func (b *TAMG) Copy() *TAMG {
	nt := new(TAMG)
	nt.tam = b.tam.Copy()
	nt.generation = b.generation
	return nt
}

// Add adds the blessing pattern to the tag in the reciever.
func (b *TAMG) Add(pattern security.BlessingPattern, tag string) {
	b.tam.Add(pattern, tag)
}
