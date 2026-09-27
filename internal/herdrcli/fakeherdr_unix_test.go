//go:build !windows

package herdrcli

// fakeHerdrName is the file name a herdr stand-in needs to be found on PATH.
const fakeHerdrName = "herdr"

// fakeHerdrScript is a herdr stand-in that prints stdout and exits 0.
func fakeHerdrScript(stdout string) string {
	return "#!/bin/sh\nprintf '%s' '" + stdout + "'\n"
}
