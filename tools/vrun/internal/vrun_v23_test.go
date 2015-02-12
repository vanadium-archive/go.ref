package main_test

//go:generate v23 test generate .

import (
	"os"

	"v.io/core/veyron/lib/testutil/v23tests"
	_ "v.io/core/veyron/profiles/static"
)

func V23TestAgentd(t *v23tests.T) {
	vrunBin := t.BuildGoPkg("v.io/core/veyron/tools/vrun")
	pingpongBin := t.BuildGoPkg("v.io/core/veyron/security/agent/pingpong")
	agentdBin := t.BuildGoPkg("v.io/core/veyron/security/agent/agentd")
	helperBin := t.BuildGoPkg("v.io/core/veyron/tools/vrun/internal")
	principalBin := t.BuildGoPkg("v.io/core/veyron/tools/principal")

	creds := t.TempDir()
	agentdBin.WithEnv("VEYRON_CREDENTIALS="+creds).Start("--no_passphrase",
		"--additional_principals="+creds,
		helperBin.Path(),
		vrunBin.Path(),
		pingpongBin.Path(),
		principalBin.Path()).WaitOrDie(os.Stdout, os.Stderr)
}
