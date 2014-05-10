// Package wire provides the types for representing ECDSA public keys, ECDSA
// Signatures, Caveats, and the various Identity implementations (described in
// veyron/runtimes/google/security) on the wire. The package also provides methods
// for encoding (decoding) the corresponding Go types to (from) wire types.
// While the wire types are themselves described as Go structs, they only make
// use of primitive types and therefore can be used in any programming language
// (assuming the language understands VOM). For example, instead of using the
// Go-specific crypto.ecdsa.PublicKey interfaces for describing ECDSA public keys,
// we define a publicKey wire type struct that only contains the primitive values
// that make up the public key.
package wire

import (
	"veyron2/security"
)

const (
	keyCurveP256 keyCurve = 0
	// ChainSeparator is used to join blessing names to form a blessing chain name.
	ChainSeparator = "/"
	// UntrustedIDProviderPrefix is the prefix added to identity names
	// when the identity provider is unknown (i.e., neither trusted nor
	// mistrusted).
	UntrustedIDProviderPrefix = "untrusted/"
)

type keyCurve byte

// PublicKey represents an ECDSA PublicKey.
type PublicKey struct {
	// Curve identifies the curve of an ECDSA PublicKey.
	Curve keyCurve
	// XY is the marshaled form of a point on the curve using the format specified
	// in section 4.3.6 of ANSI X9.62.
	XY []byte
}

// Signature represents an ECDSA signature.
type Signature struct {
	// R, S specify the pair of integers that make up an ECDSA signature.
	R, S []byte
}

// Caveat represents a veyron2/security.ServiceCaveat.
type Caveat struct {
	// Service is a pattern identifying the services that the caveat encoded in Bytes
	// is bound to.
	Service security.PrincipalPattern
	// Bytes is a serialized representation of the embedded caveat.
	Bytes []byte
}

// Certificate is a signed assertion binding a name to a public key under a certain set
// of caveats. The issuer of a Certificate is the principal that possesses the private key
// under which the Certificate was signed. The Certificate's signature is over the contents
// of the Certificate along with the Signature of the issuer.
type Certificate struct {
	// Name specified in the certificate, e.g., Alice, Bob. Name must not have the
	// character "/".
	Name string
	// PublicKey is the ECDSA public key associated with the Certificate.
	PublicKey PublicKey
	// Caveats under which the certificate is valid.
	Caveats []Caveat
	// Signature of the contents of the certificate.
	Signature Signature
}

// ChainPublicID represents the chain implementation of PublicIDs from veyron/runtimes/google/security.
// It consists of a chain of certificates such that each certificate is signed using the private key
// of the previous certificate (i.e., issuer). The certificate's signature is over its contents along
// with the signature of the issuer certificate (this is done to bind this certificate to the issuer
// chain). The first certificate of the chain is "self signed". The last certificate's public key is
// considered the PublicID's public key. The chain of certificates, if valid, effectively binds a chain
// of names to the PublicID's public key.
type ChainPublicID struct {
	// Certificates specifies the chain of certificates for the PublicID.
	Certificates []Certificate
}

// ChainPrivateID represents the chain implementation of PrivateIDs from veyron/runtimes/google/security.
type ChainPrivateID struct {
	// PublicID associated with the PrivateID.
	PublicID *ChainPublicID
	// Secret represents the secret integer that together with an ECDSA public key makes up the
	// corresponding private key.
	Secret []byte
}

// Blessing is a signed assertion binding a name to a public key under a certain set
// of caveats. The aforesaid public key is also called the "public key being blessed".
// The issuer of a blessing is the principal that possesses the private key
// under which the Blessing was signed. The PublicID of the issuer is also linked to
// from the blessing.
type Blessing struct {
	// Blessor is the PublicID of the issuer of the blessing. It is nil if the blessing
	// is self-signed, i.e, the public key being blessed and the private key signing
	// the blessing correspond.
	Blessor *TreePublicID
	// Name specified in the blessing, e.g., Alice, Bob. Name must not have the
	// characters "/" or "#".
	Name string
	// Caveats under which the blessing is valid.
	Caveats []Caveat
	// Signature of the contents of the blessing along with the public key being
	// blessed.
	Signature Signature
}

// TreePublicID represents the tree implementation of PublicIDs from veyron/runtimes/google/security.
// It consists of a public key and a list of blessings binding different names to the public key.
// For each blessing, the blesser's PublicID (which is linked to from the blessing) may in turn
// have blessings of its own thus resulting in a tree of blessings. The blessings at the leaves
// of the tree are "self signed". This blessing tree effectively binds a tree of names to the
// PublicID depending on which blessings are valid.
type TreePublicID struct {
	// PublicKey is the ECDSA public key associated with the PublicID.
	PublicKey PublicKey
	// Blessings is the list of blessings for the aforesaid public key.
	Blessings []Blessing
}

// TreePrivateID represents the tree implementation of PrivateIDs from veyron/runtimes/google/security.
type TreePrivateID struct {
	// PublicID associated with the PrivateID.
	PublicID *TreePublicID
	// Secret represents the secret integer that together with an ECDSA public key makes up the
	// corresponding private key.
	Secret []byte
}
