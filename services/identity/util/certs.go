package util

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// WriteCertAndKey creates a certificate and private key for a given host and
// duration and writes them to cert.pem and key.pem.
func WriteCertAndKey(host string, duration time.Duration) error {
	listCmd := exec.Command("go", "list", "-f", "{{.Dir}}", "crypto/tls")
	output, err := listCmd.Output()
	if err != nil {
		return fmt.Errorf("%s failed: %v", strings.Join(listCmd.Args, " "), err)
	}
	generateCertFile := filepath.Join(strings.TrimSpace(string(output)), "generate_cert.go")
	if err := exec.Command("go", "run", generateCertFile, "--host", host, "--duration", duration.String()).Run(); err != nil {
		return fmt.Errorf("Could not generate key and cert: %v", err)
	}
	return nil
}
