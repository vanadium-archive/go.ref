// Do not test on OS X as the default SSH installation that ships with that (at least up till 10.9.3) is not compiled with ECDSA key support.
// +build !darwin

package sshagent_test

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"veyron/runtimes/google/lib/sshagent"
)

func TestSSHAgent(t *testing.T) {
	if err := start(); err != nil {
		t.Fatal(err)
	}
	defer stop()
	agent, err := sshagent.New()
	if err != nil {
		t.Fatal(err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	if keys, comments, err := agent.List(); len(keys) != 0 || len(comments) != 0 || err != nil {
		t.Errorf("Got (%v, %v, %v) want ([], [], nil)", keys, comments, err)
	}
	if err := agent.Add(key, "bugs bunny"); err != nil {
		t.Error(err)
	}
	if keys, comments, err := agent.List(); len(keys) != 1 || len(comments) != 1 || err != nil || !samekey(keys[0], key) || comments[0] != "bugs bunny" {
		var match bool
		if len(keys) == 1 {
			match = samekey(keys[0], key)
		}
		t.Errorf("Got (%v, %v, %v) want ([<something>], [bugs bunny], nil), keys match: %v", keys, comments, err, match)
	}
	if r, s, err := agent.Sign(&key.PublicKey, []byte("looney tunes")); err != nil {
		t.Error(err)
	} else if !ecdsa.Verify(&key.PublicKey, sha256hash("looney tunes"), r, s) {
		t.Errorf("signature does not verify")
	}
	if err := agent.Remove(&key.PublicKey); err != nil {
		t.Error(err)
	}
	if keys, comments, err := agent.List(); len(keys) != 0 || len(comments) != 0 || err != nil {
		t.Errorf("Got (%v, %v, %v) want ([], [], nil)", keys, comments, err)
	}
}

func samekey(pub *ecdsa.PublicKey, priv *ecdsa.PrivateKey) bool {
	return reflect.DeepEqual(*pub, priv.PublicKey)
}

func sha256hash(s string) []byte {
	h := sha256.New()
	h.Write([]byte(s))
	return h.Sum(nil)
}

func start() error {
	os.Setenv(sshagent.SSH_AUTH_SOCK, "")
	os.Setenv("SSH_AGENT_PID", "")
	cmd := exec.Command("ssh-agent")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to created stdout pipe for ssh-agent: %v", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to execute ssh-agent -d: %v", err)
	}
	reader := bufio.NewReader(stdout)
	var line1, line2 string
	if line1, err = reader.ReadString('\n'); err != nil {
		return fmt.Errorf("failed to read line1 from ssh-agent: %v", err)
	}
	if line2, err = reader.ReadString('\n'); err != nil {
		return fmt.Errorf("failed to read line2 from ssh-agent: %v", err)
	}

	const (
		prefix1 = sshagent.SSH_AUTH_SOCK + "="
		prefix2 = "SSH_AGENT_PID="
	)
	os.Setenv(sshagent.SSH_AUTH_SOCK, strings.Split(strings.TrimPrefix(line1, prefix1), ";")[0])
	os.Setenv("SSH_AGENT_PID", strings.Split(strings.TrimPrefix(line2, prefix2), ";")[0])
	return nil
}

func stop() {
	exec.Command("ssh-agent", "-k").Run()
}
