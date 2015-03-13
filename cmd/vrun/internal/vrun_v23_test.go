package main_test

//go:generate v23 test generate .

import (
	"os"

	_ "v.io/x/ref/profiles/static"
	"v.io/x/ref/test/v23tests"
)

func V23TestAgentd(t *v23tests.T) {
	vrunBin := t.BuildGoPkg("v.io/x/ref/cmd/vrun")
	pingpongBin := t.BuildGoPkg("v.io/x/ref/security/agent/pingpong")
	agentdBin := t.BuildGoPkg("v.io/x/ref/security/agent/agentd")
	helperBin := t.BuildGoPkg("v.io/x/ref/cmd/vrun/internal")
	principalBin := t.BuildGoPkg("v.io/x/ref/cmd/principal")

	v23tests.RunRootMT(t, "--veyron.tcp.address=127.0.0.1:0")

	creds := t.NewTempDir()
	agentdBin.WithEnv("VEYRON_CREDENTIALS="+creds).Start("--no_passphrase",
		"--additional_principals="+creds,
		helperBin.Path(),
		vrunBin.Path(),
		pingpongBin.Path(),
		principalBin.Path()).WaitOrDie(os.Stdout, os.Stderr)
}
