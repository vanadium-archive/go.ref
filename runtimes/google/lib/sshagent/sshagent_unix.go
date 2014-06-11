package sshagent

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"encoding/binary"
	"fmt"
	"io"
	"math/big"
	"net"
	"os"
	"strings"
)

// New returns an Agent implementation that uses the environment variable SSH_AUTH_SOCK
// to configure communication with a running ssh-agent.
func New() (Agent, error) {
	sock := os.Getenv(SSH_AUTH_SOCK)
	if len(sock) == 0 {
		return nil, fmt.Errorf("SSH_AUTH_SOCK not set")
	}
	return &agent{addr: sock}, nil
}

const (
	// Requests from client to agent (Section 3.2 of PROTOCOL.agent)
	SSH2_AGENTC_REQUEST_IDENTITIES    = 11
	SSH2_AGENTC_SIGN_REQUEST          = 13
	SSH2_AGENTC_ADD_IDENTITY          = 17
	SSH2_AGENTC_REMOVE_IDENTITY       = 18
	SSH2_AGENTC_REMOVE_ALL_IDENTITIES = 19
	SSH2_AGENTC_ADD_ID_CONSTRAINED    = 25

	// Key-type independent requests from client to agent (Section 3.3 of PROTOCOL.agent)
	SSH_AGENTC_ADD_SMARTCARD_KEY    = 20
	SSH_AGENTC_REMOVE_SMARTCARD_KEY = 21
	SSH_AGENTC_LOCK                 = 22
	SSH_AGENTC_UNLOCK               = 23

	// Generic replies from agent to client (Section 3.4)
	SSH2_AGENT_FAILURE = 5
	SSH2_AGENT_SUCCESS = 6

	// Replies from agent to client for protocol 1 key operations (Section 3.6 of PROTOCOL.agent)
	SSH2_AGENT_IDENTITIES_ANSWER             = 12
	SSH2_AGENT_SIGN_RESPONSE                 = 14
	SSH_AGENTC_ADD_SMARTCARD_KEY_CONSTRAINED = 26

	// Key constraint identifiers (Section 3.7 of PROTOCOL.agent)
	SSH_AGENT_CONSTRAIN_LIFETIME = 1
	SSH_AGENT_CONSTRAIN_CONFIRM  = 2

	// SSH_AUTH_SOCK is the name of the environment variable containing the path to the
	// unix socket to communicate with the SSH agent.
	SSH_AUTH_SOCK = "SSH_AUTH_SOCK"

	ecdsaKeyPrefix = "ecdsa-sha2-"
)

var curvenames = map[string]elliptic.Curve{
	"nistp256": elliptic.P256(),
	"nistp384": elliptic.P384(),
	"nistp521": elliptic.P521(),
}

func name2curve(name string) (elliptic.Curve, error) {
	curve, exists := curvenames[name]
	if !exists {
		return nil, fmt.Errorf("unrecognized elliptic curve: %q", name)
	}
	return curve, nil
}

func curve2name(curve elliptic.Curve) (string, error) {
	for n, c := range curvenames {
		if c == curve {
			return n, nil
		}
	}
	return "", fmt.Errorf("unregistered elliptic curve: %v", curve)
}

func readU32(r io.Reader) (uint32, error) {
	var ret uint32
	err := binary.Read(r, binary.BigEndian, &ret)
	return ret, err
}

func read(r io.Reader) ([]byte, error) {
	size, err := readU32(r)
	if err != nil {
		return nil, fmt.Errorf("failed to read length header: %v", err)
	}
	msg := make([]byte, size)
	if _, err := io.ReadFull(r, msg); err != nil {
		return nil, fmt.Errorf("failed to read %d bytes: %v", size, err)
	}
	return msg, nil
}

func readString(r io.Reader) (string, error) {
	b, err := read(r)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func readcurve(r io.Reader) (elliptic.Curve, error) {
	str, err := readString(r)
	if err != nil {
		return nil, fmt.Errorf("failed to read ECDSA curve: %v", err)
	}
	return name2curve(str)
}

func write(msg []byte, w io.Writer) error {
	if err := binary.Write(w, binary.BigEndian, uint32(len(msg))); err != nil {
		return err
	}
	if n, err := w.Write(msg); err != nil {
		return err
	} else if n != len(msg) {
		return fmt.Errorf("wrote %d, wanted to write %d bytes", n, len(msg))
	}
	return nil
}

func blob2ecdsa(blob []byte) (*ecdsa.PublicKey, error) {
	buf := bytes.NewBuffer(blob)
	keytype, err := readString(buf)
	if err != nil {
		return nil, fmt.Errorf("failed to read key type: %v", err)
	}
	// the curve is specified as a key type (e.g. ecdsa-sha2-nistp256)
	// and then again as a string. The two should match.
	if !strings.HasPrefix(keytype, ecdsaKeyPrefix) {
		return nil, fmt.Errorf("not a recognized ECDSA key type: %q", keytype)
	}
	keycurve, err := name2curve(strings.TrimPrefix(keytype, ecdsaKeyPrefix))
	if err != nil {
		return nil, err
	}
	curve, err := readcurve(buf)
	if err != nil {
		return nil, err
	}
	if curve != keycurve {
		return nil, fmt.Errorf("ECDSA curve does not match key type")
	}
	marshaled, err := read(buf)
	if err != nil {
		return nil, fmt.Errorf("failed to read marshaled ECDSA point: %v", err)
	}
	x, y := elliptic.Unmarshal(curve, marshaled)
	if x == nil {
		return nil, fmt.Errorf("failed to unmarshal ECDSA point: %v", err)
	}
	return &ecdsa.PublicKey{Curve: curve, X: x, Y: y}, nil
}

type agent struct {
	addr string
	conn io.ReadWriter
}

func (a *agent) connect() error {
	if a.conn != nil {
		return nil
	}
	var err error
	if a.conn, err = net.Dial("unix", a.addr); err != nil {
		return fmt.Errorf("failed to connect to %v=%v", SSH_AUTH_SOCK, a.addr)
	}
	return nil
}

func (a *agent) call(request []byte) ([]byte, error) {
	if err := a.connect(); err != nil {
		return nil, err
	}
	if err := write(request, a.conn); err != nil {
		a.conn = nil
		return nil, fmt.Errorf("failed to send request: %v", err)
	}
	response, err := read(a.conn)
	if err != nil {
		a.conn = nil
		return nil, fmt.Errorf("failed to read response: %v", err)
	}
	return response, nil
}

// Section 2.5.2 of PROTOCOL.agent
func (a *agent) List() (keys []*ecdsa.PublicKey, comments []string, err error) {
	reply, err := a.call([]byte{SSH2_AGENTC_REQUEST_IDENTITIES})
	if err != nil {
		return nil, nil, fmt.Errorf("SSH2_AGENTC_REQUEST_IDENTITIES call failed: %v", err)
	}
	if len(reply) == 0 {
		return nil, nil, fmt.Errorf("SSH2_AGENTC_REQUEST_IDENTITES call received empty reply")
	}
	if got, want := reply[0], byte(SSH2_AGENT_IDENTITIES_ANSWER); got != want {
		return nil, nil, fmt.Errorf("unexpected reply from ssh-agent: got %v, want SSH2_AGENT_IDENTITIES_ANSWER(%v)", got, want)
	}
	buf := bytes.NewBuffer(reply[1:])
	num, err := readU32(buf)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read num identities from SSH2_AGENT_IDENTITIES_ANSWER: %v", err)
	}
	for i := uint32(0); i < num; i++ {
		blob, err := read(buf)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to read key blob for key %d of %d in SSH2_AGENT_IDENTITIES_ANSWER: %v", i, num, err)
		}
		comment, err := readString(buf)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to read comment for key %d of %d in SSH2_AGENT_IDENTITIES_ANSWER: %v", i, num, err)
		}
		key, _ := blob2ecdsa(blob)
		if key != nil {
			keys = append(keys, key)
			comments = append(comments, comment)
		}
	}
	return
}

func (a *agent) checkSSH2_AGENT_SUCCESS(request []byte, description string) error {
	reply, err := a.call(request)
	if err != nil {
		return fmt.Errorf("%v call failed: %v", description, err)
	}
	if len(reply) != 1 || reply[0] != SSH2_AGENT_SUCCESS {
		return fmt.Errorf("got reply %v for %v, wanted [SSH2_AGENT_SUCCESS(%d)]", reply, description, SSH2_AGENT_SUCCESS)
	}
	return nil
}

func bignum(n *big.Int) []byte {
	bytes := n.Bytes()
	// Prepend 0 if sign bit is set.
	// See sshbuf_put_bignum2 in ssh/sshbuf-getput-crypto.c revision 1.1:
	// http://www.openbsd.org/cgi-bin/cvsweb/src/usr.bin/ssh/sshbuf-getput-crypto.c?rev=1.1;content-type=text%2Fx-cvsweb-markup
	if bytes[0]&0x80 != 0 {
		return append([]byte{0}, bytes...)
	}
	return bytes
}

// Section 2.2.3 of PROTOCOL.agent
func (a *agent) Add(key *ecdsa.PrivateKey, comment string) error {
	if key.D.Sign() < 0 || key.PublicKey.X.Sign() < 0 || key.PublicKey.Y.Sign() < 0 {
		// As of June 2014, ssh did not like negative bignums.
		// See sshbuf_get_bignum2 in ssh/sshbuf-getput-crypto.c revision 1.1:
		// http://www.openbsd.org/cgi-bin/cvsweb/src/usr.bin/ssh/sshbuf-getput-crypto.c?rev=1.1;content-type=text%2Fx-cvsweb-markup
		return fmt.Errorf("ssh-agent does not support negative big numbers")
	}
	curve, err := curve2name(key.PublicKey.Curve)
	if err != nil {
		return err
	}
	var request bytes.Buffer
	w := func(data []byte) {
		if err == nil {
			err = write(data, &request)
		}
	}
	if err := request.WriteByte(SSH2_AGENTC_ADD_IDENTITY); err != nil {
		return err
	}
	w([]byte(ecdsaKeyPrefix + curve)) // key type: e.g., ecdsa-sha2-nistp256
	w([]byte(curve))                  // ecdsa curve: e.g. nistp256
	w(elliptic.Marshal(key.PublicKey.Curve, key.PublicKey.X, key.PublicKey.Y))
	w(bignum(key.D))
	w([]byte(comment))
	if err != nil {
		return err
	}
	return a.checkSSH2_AGENT_SUCCESS(request.Bytes(), "SSH2_AGENTC_ADD_IDENTITY")
}

// keyblob produces an encoding of an ecdsa.PublicKey as per Section 3.1 in RFC 5656,
// (extending Section 6.6 of RFC 4253),
// required by the SSH2_AGENTC_REMOVE_IDENTITY and SSH2_AGENTC_SIGN_REQUEST
// (Section 2.4.2 and Section 2.6.2 in PROTOCOL.agent).
func keyblob(key *ecdsa.PublicKey) ([]byte, error) {
	curve, err := curve2name(key.Curve)
	if err != nil {
		return nil, err
	}
	var blob bytes.Buffer
	if err := write([]byte(ecdsaKeyPrefix+curve), &blob); err != nil {
		return nil, err
	}
	if err := write([]byte(curve), &blob); err != nil {
		return nil, err
	}
	if err := write(elliptic.Marshal(key.Curve, key.X, key.Y), &blob); err != nil {
		return nil, err
	}
	return blob.Bytes(), nil
}

func (a *agent) Remove(key *ecdsa.PublicKey) error {
	keyblob, err := keyblob(key)
	if err != nil {
		return err
	}
	request := bytes.NewBuffer([]byte{SSH2_AGENTC_REMOVE_IDENTITY})
	if err := write(keyblob, request); err != nil {
		return err
	}
	return a.checkSSH2_AGENT_SUCCESS(request.Bytes(), "SSH2_AGENTC_REMOVE_IDENTITY")
}

// Section 2.6.2 of PROTOCOL.agent
func (a *agent) Sign(pub *ecdsa.PublicKey, data []byte) (r, s *big.Int, err error) {
	keyblob, err := keyblob(pub)
	if err != nil {
		return nil, nil, err
	}
	request := bytes.NewBuffer([]byte{SSH2_AGENTC_SIGN_REQUEST})
	w := func(data []byte) {
		if err == nil {
			err = write(data, request)
		}
	}
	w(keyblob)
	w(data)
	w([]byte{0, 0, 0, 0}) // flags (see Section 2.6.2 of PROTOCOL.agent)
	if err != nil {
		return nil, nil, err
	}
	reply, err := a.call(request.Bytes())
	if err != nil {
		return nil, nil, err
	}
	if len(reply) == 0 {
		return nil, nil, fmt.Errorf("empty reply for SSH2_AGENT_SIGN_REQUEST")
	}
	if reply[0] != SSH2_AGENT_SIGN_RESPONSE {
		return nil, nil, fmt.Errorf("got reply %v, want SSH2_AGENT_SIGN_RESPONSE(%d)", reply[0], SSH2_AGENT_SIGN_RESPONSE)
	}
	reply, err = read(bytes.NewBuffer(reply[1:]))
	if err != nil {
		return nil, nil, err
	}
	reader := bytes.NewBuffer(reply)
	if _, err := read(reader); err != nil { // key, e.g. ecdsa-sha2-nistp256
		return nil, nil, err
	}
	signature, err := read(reader)
	if err != nil {
		return nil, nil, err
	}
	reader = bytes.NewBuffer(signature)
	R, err := read(reader)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read signature's R: %v", err)
	}
	S, err := read(reader)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read signature's S: %v", err)
	}
	r = new(big.Int)
	s = new(big.Int)
	return r.SetBytes(R), s.SetBytes(S), nil
}
