//go:build windows

package herdrcli

// fakeHerdrName is the file name a herdr stand-in needs to be found on PATH:
// Windows resolves commands through PATHEXT, and a batch file is the simplest
// runnable stand-in.
const fakeHerdrName = "herdr.cmd"

// fakeHerdrScript is a herdr stand-in that prints stdout without a trailing
// newline and exits 0.
func fakeHerdrScript(stdout string) string {
	script := "@echo off\r\n"
	if stdout != "" {
		script += "<nul set /p =" + stdout + "\r\n"
	}
	return script + "exit /b 0\r\n"
}
