package setup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// These tests run the real bin/ensure-binary.sh in a sandbox repo whose PATH
// holds only the fakes each case needs: a uname, gh, curl, go, and digest
// shims serving from a fake release store. Every case asserts both the
// observable result and what the script touched.

const (
	ensureTestVersion = "1.2.3"
	ensureTestTag     = "v" + ensureTestVersion
	ensureTestAsset   = "usagebar-linux-amd64"
	ensureOriginal    = "original"
	ensureTestTimeout = 60 * time.Second
	ensureWaitTimeout = 20 * time.Second

	ensureTestBootID      = "6f1d2a3b-4c5d-4e6f-8a9b-0c1d2e3f4a5b"
	ensureTestPIDNS       = "pid:[4026531836]"
	ensureTestSession     = "9A8B7C6D-5E4F-4A3B-9C2D-1E0F9A8B7C6D"
	ensureTestScope       = "linux-" + ensureTestBootID + "-pidns4026531836"
	ensureTestDarwinScope = "darwin-9a8b7c6d-5e4f-4a3b-9c2d-1e0f9a8b7c6d"
	ensureOtherBootScope  = "linux-0a1b2c3d-4e5f-4a6b-8c7d-9e0f1a2b3c4d-pidns4026531836"
	ensureOtherPIDNSScope = "linux-" + ensureTestBootID + "-pidns4026531999"
)

// ensureBaseTools are the only host utilities the entrypoint and its shims may
// use. PATH is otherwise the sandbox alone, so no host gh, go, usagebar, or
// digest tool leaks into a case.
var ensureBaseTools = []string{"awk", "chmod", "cp", "dirname", "mkdir", "mktemp", "mv", "ps", "rm", "sleep", "tr"}

// ensureOpts selects which fakes the sandbox PATH provides.
type ensureOpts struct {
	version   string   // manifest version written to herdr-plugin.toml
	unameS    string   // uname -s output; "" means Linux
	gh        bool     // gh shim that serves from the release store
	ghBroken  bool     // gh shim that always fails
	curl      bool     // curl shim that serves from the release store
	verifiers []string // "sha256sum" (host digest shim) and/or "shasum" (host symlink)
	brokenSHA bool     // sha256sum shim that always exits 1
	goShim    bool     // go shim that writes the source payload to -o
	goFail    bool     // go shim that exits 1 without writing
	psBroken  bool     // ps shim that always fails
	psListing string   // ps -e -o pid= output served in place of the host ps; "" keeps the host ps
	holdGh    bool     // gh shim blocks its first call until the test releases it
	bootID    string   // boot_id served by cat; "" makes the scope unavailable
	pidNS     string   // /proc/self/ns/pid target; "" makes the scope unavailable
	session   string   // kern.bootsessionuuid served by sysctl; "" makes it unavailable
	procView  string   // /proc view of the installer's own PID served by cat and readlink; "" and "nested-valid" are matching views, other named values inject one fault
}

func ensureReleaseOpts() ensureOpts {
	return ensureOpts{
		version:   ensureTestVersion,
		gh:        true,
		curl:      true,
		verifiers: []string{"sha256sum"},
		bootID:    ensureTestBootID,
		pidNS:     ensureTestPIDNS,
		session:   ensureTestSession,
	}
}

type ensureRun struct {
	code int
	out  string
	pid  int
}

type ensureSandbox struct {
	t         *testing.T
	bash      string
	repo      string
	pathDir   string
	store     string
	marker    string
	ghLog     string
	curlLog   string
	goLog     string
	goPayload string
	hold      string
}

func newEnsureSandbox(t *testing.T, o ensureOpts) *ensureSandbox {
	t.Helper()
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skipf("bash not available: %v", err)
	}
	base := t.TempDir()
	sb := &ensureSandbox{
		t:         t,
		bash:      bash,
		repo:      filepath.Join(base, "repo"),
		pathDir:   filepath.Join(base, "path"),
		store:     filepath.Join(base, "releases"),
		marker:    filepath.Join(base, "marker"),
		ghLog:     filepath.Join(base, "gh.log"),
		curlLog:   filepath.Join(base, "curl.log"),
		goLog:     filepath.Join(base, "go.log"),
		goPayload: filepath.Join(base, "go-source-payload"),
		hold:      filepath.Join(base, "hold"),
	}
	ensureMkdir(t, filepath.Join(sb.repo, "bin"))
	ensureMkdir(t, sb.pathDir)
	ensureMkdir(t, sb.store)
	ensureMkdir(t, sb.hold)
	sb.copyEntrypoint()
	manifest := fmt.Sprintf("name = \"herdr-agent-usage\"\nversion = %q\n", o.version)
	ensureWriteFile(t, filepath.Join(sb.repo, "herdr-plugin.toml"), []byte(manifest), 0o644)
	sb.linkBaseTools()
	sb.writeShims(o)
	ensureWriteFile(t, sb.goPayload, ensurePayload("source build", 0), 0o755)
	return sb
}

func (sb *ensureSandbox) copyEntrypoint() {
	script, err := os.ReadFile(filepath.Join("..", "..", "bin", "ensure-binary.sh"))
	if err != nil {
		sb.t.Fatalf("read entrypoint: %v", err)
	}
	ensureWriteFile(sb.t, filepath.Join(sb.repo, "bin", "ensure-binary.sh"), script, 0o755)
}

func (sb *ensureSandbox) linkBaseTools() {
	for _, name := range ensureBaseTools {
		real, err := exec.LookPath(name)
		if err != nil {
			sb.t.Fatalf("host tool %s: %v", name, err)
		}
		if err := os.Symlink(real, filepath.Join(sb.pathDir, name)); err != nil {
			sb.t.Fatalf("link %s: %v", name, err)
		}
	}
}

func (sb *ensureSandbox) writeShims(o ensureOpts) {
	unameS := o.unameS
	if unameS == "" {
		unameS = "Linux"
	}
	sb.writeExecutable("uname", "#!/bin/sh\ncase \"$1\" in\n  -s) echo "+unameS+" ;;\n  -m) echo x86_64 ;;\nesac\n")
	if o.psBroken {
		sb.breakPS()
	}
	if o.psListing != "" {
		sb.servePS(o.psListing)
	}
	for _, name := range o.verifiers {
		sb.linkVerifier(name, o.brokenSHA)
	}
	if o.gh {
		sb.writeGhShim(o.ghBroken, o.holdGh)
	}
	if o.curl {
		sb.writeCurlShim()
	}
	if o.goShim {
		sb.writeGoShim(o.goFail)
	}
	sb.writeScopeShims(o)
}

// writeScopeShims serves the boot, PID-namespace, and /proc identity that
// stage_scope and proc_view_is_caller_namespace read. Each shim answers only the
// exact queries the installer makes and fails on an empty value, as the real
// source does when the identity is unavailable. o.procView selects the /proc
// view of the installer's own PID: "" and "nested-valid" serve matching views,
// and each other named value injects one fault that the installer must reject.
func (sb *ensureSandbox) writeScopeShims(o ensureOpts) {
	sb.writeExecutable("cat", `#!/bin/sh
case "$1" in
  /proc/sys/kernel/random/boot_id) [ -n '`+o.bootID+`' ] || exit 1; echo '`+o.bootID+`' ;;
  /proc/*/status)
    pid="${1#/proc/}"; pid="${pid%/status}"
    case "$pid" in ''|*[!0-9]*) exit 1 ;; esac
    case '`+o.procView+`' in
      status-unreadable) exit 1 ;;
      status-no-nspid) printf 'Name:\tinstaller\nPid:\t%s\n' "$pid" ;;
      status-two-nspid) printf 'NSpid:\t%s\t1\nNSpid:\t%s\n' "$pid" "$pid" ;;
      nspid-mismatch) printf 'NSpid:\t%s\t1\n' "$pid" ;;
      nspid-malformed) printf 'NSpid:\t%s\tzero\n' "$pid" ;;
      nested-valid) printf 'NSpid:\t%s\t%s\n' "$((pid + 1000))" "$pid" ;;
      *) printf 'NSpid:\t%s\n' "$pid" ;;
    esac ;;
  *) exit 1 ;;
esac
`)
	sb.writeExecutable("readlink", `#!/bin/sh
case "$1" in
  /proc/self/ns/pid) [ -n '`+o.pidNS+`' ] || exit 1; echo '`+o.pidNS+`' ;;
  /proc/*/ns/pid)
    case '`+o.procView+`' in
      ns-unreadable) exit 1 ;;
      ns-differs) echo 'pid:[1]' ;;
      *) [ -n '`+o.pidNS+`' ] || exit 1; echo '`+o.pidNS+`' ;;
    esac ;;
  *) exit 1 ;;
esac
`)
	sb.writeExecutable("sysctl", "#!/bin/sh\n[ \"$1 $2\" = \"-n kern.bootsessionuuid\" ] && [ -n '"+o.session+"' ] || exit 1\necho '"+o.session+"'\n")
}

func (sb *ensureSandbox) linkVerifier(name string, broken bool) {
	switch name {
	case "sha256sum":
		sb.writeSha256sumShim(broken)
	case "shasum":
		real, err := exec.LookPath("shasum")
		if err != nil {
			sb.t.Skipf("host shasum not available: %v", err)
		}
		if err := os.Symlink(real, filepath.Join(sb.pathDir, "shasum")); err != nil {
			sb.t.Fatalf("link shasum: %v", err)
		}
	default:
		sb.t.Fatalf("unknown verifier %q", name)
	}
}

// writeSha256sumShim stands in for GNU sha256sum, which macOS lacks, by
// delegating to the host digest tool. Output format matches GNU sha256sum.
func (sb *ensureSandbox) writeSha256sumShim(broken bool) {
	if broken {
		sb.writeExecutable("sha256sum", "#!/bin/sh\nexit 1\n")
		return
	}
	real, args := ensureHostSHA256(sb.t)
	sb.writeExecutable("sha256sum", "#!/bin/sh\nexec "+ensureShQuote(real)+" "+args+"\"$@\"\n")
}

// ensureHostSHA256 returns a host digest tool and the arguments that make it
// print the GNU sha256sum format. The test skips when the host has neither.
func ensureHostSHA256(t *testing.T) (string, string) {
	t.Helper()
	if real, err := exec.LookPath("sha256sum"); err == nil {
		return real, ""
	}
	if real, err := exec.LookPath("shasum"); err == nil {
		return real, "-a 256 "
	}
	t.Skip("no host SHA-256 tool available")
	return "", ""
}

// writeGhShim serves "gh release download <tag> --pattern <asset> -O <dest>"
// from the release store and logs every invocation. With hold set, the first
// call blocks until the test writes the release file, so the test can act
// while the download is in flight.
func (sb *ensureSandbox) writeGhShim(broken, hold bool) {
	body := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + ensureShQuote(sb.ghLog) + "\n"
	if hold {
		body += "if mkdir " + ensureShQuote(sb.hold) + "/taken 2>/dev/null; then\n" +
			"  : > " + ensureShQuote(sb.hold) + "/ready\n" +
			"  i=0\n" +
			"  while [ ! -e " + ensureShQuote(sb.hold) + "/release ] && [ $i -lt 600 ]; do sleep 0.05; i=$((i+1)); done\n" +
			"fi\n"
	}
	if broken {
		body += "exit 1\n"
	} else {
		body += "if [ \"$1\" != release ] || [ \"$2\" != download ]; then exit 1; fi\n" +
			"tag=\"$3\"; shift 3\n" +
			"pattern=\"\"; out=\"\"\n" +
			"while [ $# -gt 0 ]; do\n" +
			"  case \"$1\" in\n" +
			"    --pattern) pattern=\"$2\" ;;\n" +
			"    -O) out=\"$2\" ;;\n" +
			"  esac\n" +
			"  shift 2\n" +
			"done\n" +
			"cp " + ensureShQuote(sb.store) + "/\"$tag\"/\"$pattern\" \"$out\"\n"
	}
	sb.writeExecutable("gh", body)
}

// writeCurlShim serves "curl -fsSL <release-url> -o <dest>" from the release
// store and logs every invocation.
func (sb *ensureSandbox) writeCurlShim() {
	body := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + ensureShQuote(sb.curlLog) + "\n" +
		"url=\"$2\"; dest=\"$4\"\n" +
		"rest=\"${url#*/releases/download/}\"\n" +
		"tag=\"${rest%%/*}\"; asset=\"${rest#*/}\"\n" +
		"cp " + ensureShQuote(sb.store) + "/\"$tag\"/\"$asset\" \"$dest\"\n"
	sb.writeExecutable("curl", body)
}

// writeGoShim copies the source payload to the -o target, as `go build` would.
func (sb *ensureSandbox) writeGoShim(broken bool) {
	body := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + ensureShQuote(sb.goLog) + "\n"
	if broken {
		body += "exit 1\n"
	} else {
		body += "out=\"\"\n" +
			"while [ $# -gt 0 ]; do\n" +
			"  if [ \"$1\" = -o ]; then out=\"$2\"; shift; fi\n" +
			"  shift\n" +
			"done\n" +
			"cp " + ensureShQuote(sb.goPayload) + " \"$out\"\n"
	}
	sb.writeExecutable("go", body)
}

// breakPS swaps the linked host ps for a shim that always fails. The link is
// removed first, because writing through it would overwrite the host binary.
func (sb *ensureSandbox) breakPS() {
	if err := os.Remove(filepath.Join(sb.pathDir, "ps")); err != nil {
		sb.t.Fatalf("unlink ps: %v", err)
	}
	sb.writeExecutable("ps", "#!/bin/sh\nexit 1\n")
}

// servePS swaps the linked host ps for a shim that answers only the installer's
// `ps -e -o pid=` query with the given listing. The link is removed first, as in
// breakPS.
func (sb *ensureSandbox) servePS(listing string) {
	if err := os.Remove(filepath.Join(sb.pathDir, "ps")); err != nil {
		sb.t.Fatalf("unlink ps: %v", err)
	}
	sb.writeExecutable("ps", "#!/bin/sh\n[ \"$*\" = '-e -o pid=' ] || exit 1\nprintf '%s' "+ensureShQuote(listing)+"\n")
}

func (sb *ensureSandbox) writeExecutable(name, body string) {
	ensureWriteFile(sb.t, filepath.Join(sb.pathDir, name), []byte(body), 0o755)
}

// publish places an asset or SHA256SUMS in the fake release store under tag.
func (sb *ensureSandbox) publish(tag, name string, data []byte) {
	dir := filepath.Join(sb.store, tag)
	ensureMkdir(sb.t, dir)
	ensureWriteFile(sb.t, filepath.Join(dir, name), data, 0o644)
}

func (sb *ensureSandbox) binPath() string {
	return filepath.Join(sb.repo, "bin", "usagebar")
}

// seedBin places a pre-existing bin/usagebar with the given mode and content.
func (sb *ensureSandbox) seedBin(perm os.FileMode, content string) {
	ensureWriteFile(sb.t, sb.binPath(), []byte(content), perm)
}

// run executes the entrypoint with an isolated environment. env adds
// variables on top of PATH and the marker path, so nothing else leaks in.
func (sb *ensureSandbox) run(env []string, args ...string) ensureRun {
	sb.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), ensureTestTimeout)
	defer cancel()
	script := filepath.Join(sb.repo, "bin", "ensure-binary.sh")
	cmd := exec.CommandContext(ctx, sb.bash, append([]string{script}, args...)...)
	cmd.Dir = sb.repo
	cmd.Env = append([]string{"PATH=" + sb.pathDir, "ENSURE_TEST_MARKER=" + sb.marker}, env...)
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		sb.t.Fatalf("ensure-binary timed out after %s; output:\n%s", ensureTestTimeout, out)
	}
	if err == nil {
		return ensureRun{code: 0, out: string(out), pid: cmd.Process.Pid}
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		sb.t.Fatalf("run ensure-binary: %v", err)
	}
	return ensureRun{code: exitErr.ExitCode(), out: string(out), pid: cmd.Process.Pid}
}

func (sb *ensureSandbox) assertExit(r ensureRun, want int) {
	sb.t.Helper()
	if r.code != want {
		sb.t.Fatalf("exit = %d, want %d\noutput:\n%s", r.code, want, r.out)
	}
}

// assertNotExecuted fails when any payload ran. Payloads append to the marker.
func (sb *ensureSandbox) assertNotExecuted() {
	sb.t.Helper()
	if _, err := os.Stat(sb.marker); err == nil {
		data, _ := os.ReadFile(sb.marker)
		sb.t.Fatalf("a payload executed; marker contains %q", data)
	}
}

// assertSanityRunOnly checks that the only execution was the post-verification
// "version" check, so the payload ran exactly once and nothing else.
func (sb *ensureSandbox) assertSanityRunOnly() {
	sb.t.Helper()
	data, err := os.ReadFile(sb.marker)
	if err != nil {
		sb.t.Fatalf("payload was never run for its sanity check: %v", err)
	}
	if string(data) != "version\n" {
		sb.t.Fatalf("payload executions = %q, want only \"version\\n\"", data)
	}
}

// assertBin checks the exact content and permission bits of bin/usagebar.
func (sb *ensureSandbox) assertBin(content string, perm os.FileMode) {
	sb.t.Helper()
	data, err := os.ReadFile(sb.binPath())
	if err != nil {
		sb.t.Fatalf("read bin/usagebar: %v", err)
	}
	if !bytes.Equal(data, []byte(content)) {
		sb.t.Fatalf("bin/usagebar content = %q, want %q", data, content)
	}
	info, err := os.Stat(sb.binPath())
	if err != nil {
		sb.t.Fatalf("stat bin/usagebar: %v", err)
	}
	if info.Mode().Perm() != perm {
		sb.t.Fatalf("bin/usagebar mode = %v, want %v", info.Mode().Perm(), perm)
	}
}

// assertInstalled checks that a verified release binary reached bin/usagebar
// with its exact content and at least one execute bit.
func (sb *ensureSandbox) assertInstalled(content string) {
	sb.t.Helper()
	data, err := os.ReadFile(sb.binPath())
	if err != nil {
		sb.t.Fatalf("read bin/usagebar: %v", err)
	}
	if !bytes.Equal(data, []byte(content)) {
		sb.t.Fatalf("bin/usagebar content = %q, want %q", data, content)
	}
	info, err := os.Stat(sb.binPath())
	if err != nil {
		sb.t.Fatalf("stat bin/usagebar: %v", err)
	}
	if info.Mode()&0o111 == 0 {
		sb.t.Fatalf("bin/usagebar is not executable: mode %v", info.Mode())
	}
}

// assertBinEntries checks that bin/ holds exactly the entrypoint plus extra.
// A leftover staging directory fails this check.
func (sb *ensureSandbox) assertBinEntries(extra ...string) {
	sb.t.Helper()
	want := append([]string{"ensure-binary.sh"}, extra...)
	sort.Strings(want)
	entries, err := os.ReadDir(filepath.Join(sb.repo, "bin"))
	if err != nil {
		sb.t.Fatalf("read bin/: %v", err)
	}
	got := make([]string, 0, len(entries))
	for _, entry := range entries {
		got = append(got, entry.Name())
	}
	sort.Strings(got)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		sb.t.Fatalf("bin/ entries = %v, want %v", got, want)
	}
}

// requestLines returns the recorded invocations in a shim log.
func (sb *ensureSandbox) requestLines(logPath string) []string {
	sb.t.Helper()
	data, err := os.ReadFile(logPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		sb.t.Fatalf("read %s: %v", logPath, err)
	}
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\n")
}

// assertNoRequests checks that neither gh nor curl was invoked.
func (sb *ensureSandbox) assertNoRequests() {
	sb.t.Helper()
	if lines := append(sb.requestLines(sb.ghLog), sb.requestLines(sb.curlLog)...); len(lines) != 0 {
		sb.t.Fatalf("expected no release requests, got %q", lines)
	}
}

// assertExactTagRequests checks that every release request names exactly the
// manifest tag and never a moving "latest" target.
func (sb *ensureSandbox) assertExactTagRequests() {
	sb.t.Helper()
	lines := append(sb.requestLines(sb.ghLog), sb.requestLines(sb.curlLog)...)
	if len(lines) == 0 {
		sb.t.Fatal("no release request was made")
	}
	for _, line := range lines {
		if strings.Contains(line, "latest") || !strings.Contains(line, ensureTestTag) {
			sb.t.Fatalf("release request must target exactly %s: %q", ensureTestTag, line)
		}
	}
}

// expectRejected asserts the unverified-release contract: exit 127, the payload
// never ran, the original binary is intact, and no staging leftovers remain.
func (sb *ensureSandbox) expectRejected(r ensureRun) {
	sb.t.Helper()
	sb.assertExit(r, 127)
	sb.assertNotExecuted()
	sb.assertBin(ensureOriginal, 0o644)
	sb.assertBinEntries("usagebar")
}

// ensurePayload is a fake usagebar. Each run appends its arguments to the
// marker and exits with the given code.
func ensurePayload(label string, exit int) []byte {
	return []byte(fmt.Sprintf("#!/bin/sh\n# %s\necho \"$*\" >> \"$ENSURE_TEST_MARKER\"\nexit %d\n", label, exit))
}

// ensureSums returns a SHA256SUMS line for the test asset. sep is "  " for text
// mode or " *" for binary mode.
func ensureSums(payload []byte, sep string) []byte {
	return ensureSumsFor(ensureTestAsset, payload, sep)
}

// ensureSumsFor is ensureSums for an asset other than the Linux test asset.
func ensureSumsFor(asset string, payload []byte, sep string) []byte {
	return []byte(ensureSha256Hex(payload) + sep + asset + "\n")
}

func ensureSha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func ensureShQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func ensureMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
}

// ensureWriteFile writes data and then sets perm exactly, bypassing umask.
func ensureWriteFile(t *testing.T, path string, data []byte, perm os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, data, perm); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	if err := os.Chmod(path, perm); err != nil {
		t.Fatalf("chmod %s: %v", path, err)
	}
}

func TestEnsureBinary_VerifiedInstallViaGh(t *testing.T) {
	o := ensureReleaseOpts()
	o.verifiers = []string{"sha256sum", "shasum"}
	sb := newEnsureSandbox(t, o)
	payload := ensurePayload("release", 0)
	sb.publish(ensureTestTag, ensureTestAsset, payload)
	sb.publish(ensureTestTag, "SHA256SUMS", ensureSums(payload, "  "))

	r := sb.run(nil)
	sb.assertExit(r, 0)
	sb.assertInstalled(string(payload))
	sb.assertSanityRunOnly()
	sb.assertBinEntries("usagebar")
	sb.assertExactTagRequests()
	if n := len(sb.requestLines(sb.curlLog)); n != 0 {
		t.Fatalf("curl was requested %d times although gh served the release", n)
	}
}

func TestEnsureBinary_VerifiedInstallViaCurlBinaryModeShasum(t *testing.T) {
	sb := newEnsureSandbox(t, ensureOpts{
		version:   ensureTestVersion,
		curl:      true,
		verifiers: []string{"shasum"},
	})
	payload := ensurePayload("release", 0)
	sb.publish(ensureTestTag, ensureTestAsset, payload)
	sb.publish(ensureTestTag, "SHA256SUMS", ensureSums(payload, " *"))

	r := sb.run(nil)
	sb.assertExit(r, 0)
	sb.assertInstalled(string(payload))
	sb.assertSanityRunOnly()
	sb.assertBinEntries("usagebar")
	sb.assertExactTagRequests()
	if n := len(sb.requestLines(sb.ghLog)); n != 0 {
		t.Fatalf("gh is absent but was invoked %d times", n)
	}
	if n := len(sb.requestLines(sb.curlLog)); n != 2 {
		t.Fatalf("curl requests = %d, want 2 (asset and SHA256SUMS)", n)
	}
}

func TestEnsureBinary_VerifiedInstallAfterGhFailsUppercaseDigest(t *testing.T) {
	o := ensureReleaseOpts()
	o.ghBroken = true
	sb := newEnsureSandbox(t, o)
	payload := ensurePayload("release", 0)
	sb.publish(ensureTestTag, ensureTestAsset, payload)
	upper := strings.ToUpper(ensureSha256Hex(payload))
	sb.publish(ensureTestTag, "SHA256SUMS", []byte(upper+"  "+ensureTestAsset+"\n"))

	r := sb.run(nil)
	sb.assertExit(r, 0)
	sb.assertInstalled(string(payload))
	sb.assertSanityRunOnly()
	sb.assertBinEntries("usagebar")
	sb.assertExactTagRequests()
	if n := len(sb.requestLines(sb.ghLog)); n != 2 {
		t.Fatalf("gh attempts = %d, want 2", n)
	}
	if n := len(sb.requestLines(sb.curlLog)); n != 2 {
		t.Fatalf("curl fallback requests = %d, want 2", n)
	}
}

func TestEnsureBinary_RejectsUnverifiedReleaseAndKeepsOriginal(t *testing.T) {
	payload := ensurePayload("release", 0)
	digest := ensureSha256Hex(payload)
	zeros := strings.Repeat("0", 64)
	cases := []struct {
		name      string
		omitAsset bool
		sums      []string // nil means SHA256SUMS is not published
	}{
		{
			name: "wrong digest",
			sums: []string{zeros + "  " + ensureTestAsset},
		},
		{
			name: "short digest",
			sums: []string{digest[:63] + "  " + ensureTestAsset},
		},
		{
			name: "non-hex digest",
			sums: []string{"g" + digest[1:] + "  " + ensureTestAsset},
		},
		{
			name: "single space separator",
			sums: []string{digest + " " + ensureTestAsset},
		},
		{
			name: "valid record then single-space duplicate",
			sums: []string{digest + "  " + ensureTestAsset, digest + " " + ensureTestAsset},
		},
		{
			name: "valid record then malformed digest duplicate",
			sums: []string{digest + "  " + ensureTestAsset, digest[:63] + "  " + ensureTestAsset},
		},
		{
			name: "valid record then extra-field duplicate",
			sums: []string{digest + "  " + ensureTestAsset, digest + "  " + ensureTestAsset + " extra"},
		},
		{
			name: "misleading names only",
			sums: []string{
				digest + "  xusagebar-linux-amd64",
				digest + "  usagebar-linux-amd64.sig",
				digest + "  usagebar-linux-amd64-old",
				digest + "  dist/usagebar-linux-amd64",
			},
		},
		{
			name: "selected entry missing",
			sums: []string{digest + "  usagebar-darwin-arm64"},
		},
		{
			name: "duplicate identical records",
			sums: []string{digest + "  " + ensureTestAsset, digest + "  " + ensureTestAsset},
		},
		{
			name: "SHA256SUMS not published",
			sums: nil,
		},
		{
			name:      "binary missing at exact tag",
			omitAsset: true,
			sums:      []string{digest + "  " + ensureTestAsset},
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			sb := newEnsureSandbox(t, ensureReleaseOpts())
			if !tc.omitAsset {
				sb.publish(ensureTestTag, ensureTestAsset, payload)
			}
			if tc.sums != nil {
				sb.publish(ensureTestTag, "SHA256SUMS", []byte(strings.Join(tc.sums, "\n")+"\n"))
			}
			sb.seedBin(0o644, ensureOriginal)

			r := sb.run(nil, "--in-tree")
			sb.expectRejected(r)
			sb.assertExactTagRequests()
		})
	}
}

func TestEnsureBinary_IgnoresMisleadingRecordsBesideSelectedRecord(t *testing.T) {
	sb := newEnsureSandbox(t, ensureReleaseOpts())
	payload := ensurePayload("release", 0)
	digest := ensureSha256Hex(payload)
	sb.publish(ensureTestTag, ensureTestAsset, payload)
	sb.publish(ensureTestTag, "SHA256SUMS", []byte(strings.Join([]string{
		digest + "  " + ensureTestAsset,
		digest + "  xusagebar-linux-amd64",
		digest + "  usagebar-linux-amd64.sig",
		digest + "  usagebar-linux-amd64-old",
		digest + "  dist/usagebar-linux-amd64",
	}, "\n")+"\n"))

	r := sb.run(nil)
	sb.assertExit(r, 0)
	sb.assertInstalled(string(payload))
	sb.assertSanityRunOnly()
	sb.assertBinEntries("usagebar")
	sb.assertExactTagRequests()
}

func TestEnsureBinary_RejectsVerifiedBinaryThatDoesNotRun(t *testing.T) {
	sb := newEnsureSandbox(t, ensureReleaseOpts())
	payload := ensurePayload("release", 3)
	sb.publish(ensureTestTag, ensureTestAsset, payload)
	sb.publish(ensureTestTag, "SHA256SUMS", ensureSums(payload, "  "))
	sb.seedBin(0o644, ensureOriginal)

	r := sb.run(nil)
	sb.assertExit(r, 127)
	sb.assertSanityRunOnly()
	sb.assertBin(ensureOriginal, 0o644)
	sb.assertBinEntries("usagebar")
	sb.assertExactTagRequests()
}

func TestEnsureBinary_IgnoresDecoyReleaseOnOtherTag(t *testing.T) {
	sb := newEnsureSandbox(t, ensureReleaseOpts())
	payload := ensurePayload("decoy", 0)
	sb.publish("latest", ensureTestAsset, payload)
	sb.publish("latest", "SHA256SUMS", ensureSums(payload, "  "))
	sb.seedBin(0o644, ensureOriginal)

	r := sb.run(nil)
	sb.expectRejected(r)
	sb.assertExactTagRequests()
}

func TestEnsureBinary_ReportsUnreachableRelease(t *testing.T) {
	sb := newEnsureSandbox(t, ensureReleaseOpts())
	sb.seedBin(0o644, ensureOriginal)

	r := sb.run(nil)
	sb.expectRejected(r)
	if len(sb.requestLines(sb.ghLog)) == 0 || len(sb.requestLines(sb.curlLog)) == 0 {
		t.Fatal("both gh and curl should have been tried before giving up")
	}
	sb.assertExactTagRequests()
}

func TestEnsureBinary_RejectsWhenSha256ToolFails(t *testing.T) {
	o := ensureReleaseOpts()
	o.brokenSHA = true
	sb := newEnsureSandbox(t, o)
	payload := ensurePayload("release", 0)
	sb.publish(ensureTestTag, ensureTestAsset, payload)
	sb.publish(ensureTestTag, "SHA256SUMS", ensureSums(payload, "  "))
	sb.seedBin(0o644, ensureOriginal)

	r := sb.run(nil)
	sb.expectRejected(r)
	sb.assertExactTagRequests()
}

func TestEnsureBinary_RequiresSha256ToolBeforeNetwork(t *testing.T) {
	o := ensureReleaseOpts()
	o.verifiers = nil
	sb := newEnsureSandbox(t, o)
	payload := ensurePayload("release", 0)
	sb.publish(ensureTestTag, ensureTestAsset, payload)
	sb.publish(ensureTestTag, "SHA256SUMS", ensureSums(payload, "  "))

	r := sb.run(nil)
	sb.assertExit(r, 127)
	sb.assertNoRequests()
	sb.assertNotExecuted()
	sb.assertBinEntries()
}

func TestEnsureBinary_RefusesEmptyManifestVersion(t *testing.T) {
	o := ensureReleaseOpts()
	o.version = ""
	sb := newEnsureSandbox(t, o)
	payload := ensurePayload("release", 0)
	// Publish under the tag an empty version would yield, to prove it is never requested.
	sb.publish("v", ensureTestAsset, payload)
	sb.publish("v", "SHA256SUMS", ensureSums(payload, "  "))

	r := sb.run(nil)
	sb.assertExit(r, 127)
	sb.assertNoRequests()
	sb.assertNotExecuted()
	sb.assertBinEntries()
}

func TestEnsureBinary_RefusesUnsupportedPlatform(t *testing.T) {
	o := ensureReleaseOpts()
	o.unameS = "FreeBSD"
	sb := newEnsureSandbox(t, o)
	payload := ensurePayload("release", 0)
	sb.publish(ensureTestTag, ensureTestAsset, payload)
	sb.publish(ensureTestTag, "SHA256SUMS", ensureSums(payload, "  "))

	r := sb.run(nil)
	sb.assertExit(r, 127)
	sb.assertNoRequests()
	sb.assertNotExecuted()
	sb.assertBinEntries()
}

func TestEnsureBinary_SourceBuildRunsBeforeRelease(t *testing.T) {
	o := ensureReleaseOpts()
	o.goShim = true
	sb := newEnsureSandbox(t, o)
	payload := ensurePayload("release", 0)
	sb.publish(ensureTestTag, ensureTestAsset, payload)
	sb.publish(ensureTestTag, "SHA256SUMS", ensureSums(payload, "  "))

	r := sb.run(nil)
	sb.assertExit(r, 0)
	sb.assertInstalled(string(ensurePayload("source build", 0)))
	sb.assertNotExecuted()
	sb.assertNoRequests()
}

func TestEnsureBinary_BuildModeNeverDownloads(t *testing.T) {
	t.Run("source build succeeds", func(t *testing.T) {
		o := ensureReleaseOpts()
		o.goShim = true
		sb := newEnsureSandbox(t, o)
		payload := ensurePayload("release", 0)
		sb.publish(ensureTestTag, ensureTestAsset, payload)
		sb.publish(ensureTestTag, "SHA256SUMS", ensureSums(payload, "  "))

		r := sb.run(nil, "--build")
		sb.assertExit(r, 0)
		sb.assertInstalled(string(ensurePayload("source build", 0)))
		sb.assertNoRequests()
		sb.assertNotExecuted()
	})
	t.Run("source build fails", func(t *testing.T) {
		o := ensureReleaseOpts()
		o.goShim = true
		o.goFail = true
		sb := newEnsureSandbox(t, o)
		payload := ensurePayload("release", 0)
		sb.publish(ensureTestTag, ensureTestAsset, payload)
		sb.publish(ensureTestTag, "SHA256SUMS", ensureSums(payload, "  "))

		r := sb.run(nil, "--build")
		sb.assertExit(r, 1)
		sb.assertNoRequests()
		sb.assertNotExecuted()
		sb.assertBinEntries()
	})
}

func TestEnsureBinary_DefaultFallsBackToExactReleaseWhenGoFails(t *testing.T) {
	o := ensureReleaseOpts()
	o.goShim = true
	o.goFail = true
	sb := newEnsureSandbox(t, o)
	payload := ensurePayload("release", 0)
	sb.publish(ensureTestTag, ensureTestAsset, payload)
	sb.publish(ensureTestTag, "SHA256SUMS", ensureSums(payload, "  "))

	r := sb.run(nil)
	sb.assertExit(r, 0)
	sb.assertInstalled(string(payload))
	sb.assertSanityRunOnly()
	sb.assertBinEntries("usagebar")
	sb.assertExactTagRequests()
	if n := len(sb.requestLines(sb.goLog)); n != 1 {
		t.Fatalf("go invocations = %d, want 1 failed attempt", n)
	}
}

func TestEnsureBinary_ExistingBinaryResolutionNeverProvisions(t *testing.T) {
	t.Run("in-tree binary, default mode", func(t *testing.T) {
		sb := newEnsureSandbox(t, ensureReleaseOpts())
		sb.seedBin(0o755, "existing")

		r := sb.run(nil)
		sb.assertExit(r, 0)
		sb.assertBin("existing", 0o755)
		sb.assertNoRequests()
		sb.assertBinEntries("usagebar")
	})
	t.Run("in-tree binary, --in-tree", func(t *testing.T) {
		sb := newEnsureSandbox(t, ensureReleaseOpts())
		sb.seedBin(0o755, "existing")

		r := sb.run(nil, "--in-tree")
		sb.assertExit(r, 0)
		sb.assertBin("existing", 0o755)
		sb.assertNoRequests()
		sb.assertBinEntries("usagebar")
	})
	t.Run("USAGEBAR_BIN override", func(t *testing.T) {
		sb := newEnsureSandbox(t, ensureReleaseOpts())
		override := filepath.Join(filepath.Dir(sb.repo), "override-usagebar")
		ensureWriteFile(t, override, []byte("#!/bin/sh\nexit 0\n"), 0o755)

		r := sb.run([]string{"USAGEBAR_BIN=" + override})
		sb.assertExit(r, 0)
		sb.assertNoRequests()
		sb.assertBinEntries()
	})
	t.Run("usagebar on PATH", func(t *testing.T) {
		sb := newEnsureSandbox(t, ensureReleaseOpts())
		sb.writeExecutable("usagebar", "#!/bin/sh\nexit 0\n")

		r := sb.run(nil)
		sb.assertExit(r, 0)
		sb.assertNoRequests()
		sb.assertBinEntries()
	})
}

func TestEnsureBinary_InTreeInstallIgnoresOverrideAndPathBinary(t *testing.T) {
	sb := newEnsureSandbox(t, ensureReleaseOpts())
	override := filepath.Join(filepath.Dir(sb.repo), "override-usagebar")
	ensureWriteFile(t, override, []byte("#!/bin/sh\nexit 0\n"), 0o755)
	sb.writeExecutable("usagebar", "#!/bin/sh\nexit 0\n")
	payload := ensurePayload("release", 0)
	sb.publish(ensureTestTag, ensureTestAsset, payload)
	sb.publish(ensureTestTag, "SHA256SUMS", ensureSums(payload, "  "))

	r := sb.run([]string{"USAGEBAR_BIN=" + override}, "--in-tree")
	sb.assertExit(r, 0)
	sb.assertInstalled(string(payload))
	sb.assertSanityRunOnly()
	sb.assertBinEntries("usagebar")
	sb.assertExactTagRequests()
}

// seedStage creates a staging directory holding a partial download, the
// leftover a SIGKILLed install leaves behind. It returns the partial file.
func (sb *ensureSandbox) seedStage(name string) string {
	sb.t.Helper()
	dir := filepath.Join(sb.repo, "bin", name)
	ensureMkdir(sb.t, dir)
	partial := filepath.Join(dir, "partial")
	ensureWriteFile(sb.t, partial, []byte("partial"), 0o644)
	return partial
}

// startLiveProcess runs a host sleep until the test ends, standing in for an
// installer that is still running. It returns the process ID.
func startLiveProcess(t *testing.T) int {
	t.Helper()
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Skipf("host sleep not available: %v", err)
	}
	cmd := exec.Command(sleep, "60")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start live process: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	return cmd.Process.Pid
}

// stageDirName returns the staging directory name the installer gives a
// download by owner PID owner in scope.
func stageDirName(scope string, owner int, random string) string {
	return fmt.Sprintf(".usagebar-download.%s.%d.%s", scope, owner, random)
}

// TestEnsureBinary_ReclaimsStageOfDeadInstaller uses owner PID 99999999, which
// exceeds every platform's PID range (macOS caps PIDs at 99999, Linux at
// 4194304), so no live process can own it.
func TestEnsureBinary_ReclaimsStageOfDeadInstaller(t *testing.T) {
	sb := newEnsureSandbox(t, ensureReleaseOpts())
	sb.seedStage(stageDirName(ensureTestScope, 99999999, "ABCDEF"))
	payload := ensurePayload("release", 0)
	sb.publish(ensureTestTag, ensureTestAsset, payload)
	sb.publish(ensureTestTag, "SHA256SUMS", ensureSums(payload, "  "))

	r := sb.run(nil, "--in-tree")
	sb.assertExit(r, 0)
	sb.assertInstalled(string(payload))
	sb.assertSanityRunOnly()
	sb.assertBinEntries("usagebar")
	sb.assertExactTagRequests()
}

func TestEnsureBinary_ReclaimsStageOfDeadInstallerOnDarwin(t *testing.T) {
	o := ensureReleaseOpts()
	o.unameS = "Darwin"
	sb := newEnsureSandbox(t, o)
	const asset = "usagebar-darwin-amd64"
	sb.seedStage(stageDirName(ensureTestDarwinScope, 99999999, "ABCDEF"))
	payload := ensurePayload("release", 0)
	sb.publish(ensureTestTag, asset, payload)
	sb.publish(ensureTestTag, "SHA256SUMS", ensureSumsFor(asset, payload, "  "))

	r := sb.run(nil, "--in-tree")
	sb.assertExit(r, 0)
	sb.assertInstalled(string(payload))
	sb.assertBinEntries("usagebar")
}

// TestEnsureBinary_KeepsStageOfLiveInstaller checks that a stage whose owner
// PID is alive in this scope is not touched.
func TestEnsureBinary_KeepsStageOfLiveInstaller(t *testing.T) {
	sb := newEnsureSandbox(t, ensureReleaseOpts())
	stage := stageDirName(ensureTestScope, startLiveProcess(t), "ABCDEF")
	partial := sb.seedStage(stage)
	payload := ensurePayload("release", 0)
	sb.publish(ensureTestTag, ensureTestAsset, payload)
	sb.publish(ensureTestTag, "SHA256SUMS", ensureSums(payload, "  "))

	r := sb.run(nil, "--in-tree")
	sb.assertExit(r, 0)
	sb.assertInstalled(string(payload))
	sb.assertBinEntries("usagebar", stage)
	if _, err := os.Stat(partial); err != nil {
		t.Fatalf("live installer's staging directory was touched: %v", err)
	}
}

// TestEnsureBinary_KeepsStagesOfOtherBootsAndNamespaces checks that a PID is
// judged only inside the scope that issued it. Each owner here is absent from
// this namespace's process listing, but its PID may be live in the boot or PID
// namespace that created the stage.
func TestEnsureBinary_KeepsStagesOfOtherBootsAndNamespaces(t *testing.T) {
	sb := newEnsureSandbox(t, ensureReleaseOpts())
	stages := []string{
		stageDirName(ensureOtherBootScope, 99999999, "ABCDEF"),
		stageDirName(ensureOtherPIDNSScope, 99999999, "GHIJKL"),
		stageDirName(ensureTestDarwinScope, 99999999, "MNOPQR"),
	}
	var partials []string
	for _, stage := range stages {
		partials = append(partials, sb.seedStage(stage))
	}
	payload := ensurePayload("release", 0)
	sb.publish(ensureTestTag, ensureTestAsset, payload)
	sb.publish(ensureTestTag, "SHA256SUMS", ensureSums(payload, "  "))

	r := sb.run(nil, "--in-tree")
	sb.assertExit(r, 0)
	sb.assertInstalled(string(payload))
	sb.assertBinEntries(append([]string{"usagebar"}, stages...)...)
	for _, partial := range partials {
		if _, err := os.Stat(partial); err != nil {
			t.Fatalf("stage of another scope was removed: %v", err)
		}
	}
}

// TestEnsureBinary_KeepsEntriesThatCannotBeProvenDead covers the entries the
// reclaim rule must never touch: legacy and owner-only names, unscoped names,
// non-canonical owners, symlinks, and regular files.
func TestEnsureBinary_KeepsEntriesThatCannotBeProvenDead(t *testing.T) {
	sb := newEnsureSandbox(t, ensureReleaseOpts())
	legacyName := ".usagebar-download.ABCDEF"
	ownerOnlyName := ".usagebar-download.99999999.ABCDEF"
	unscopedName := ".usagebar-download.unscoped.GHIJKL"
	noncanonicalName := fmt.Sprintf(".usagebar-download.%s.0099.STUVWX", ensureTestScope)
	fileName := stageDirName(ensureTestScope, 99999999, "MNOPQR")
	linkName := stageDirName(ensureTestScope, 99999999, "YZABCD")
	legacy := sb.seedStage(legacyName)
	ownerOnly := sb.seedStage(ownerOnlyName)
	unscoped := sb.seedStage(unscopedName)
	noncanonical := sb.seedStage(noncanonicalName)
	file := filepath.Join(sb.repo, "bin", fileName)
	ensureWriteFile(t, file, []byte("file"), 0o644)
	target := t.TempDir()
	keep := filepath.Join(target, "keep")
	ensureWriteFile(t, keep, []byte("keep"), 0o644)
	if err := os.Symlink(target, filepath.Join(sb.repo, "bin", linkName)); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	payload := ensurePayload("release", 0)
	sb.publish(ensureTestTag, ensureTestAsset, payload)
	sb.publish(ensureTestTag, "SHA256SUMS", ensureSums(payload, "  "))

	r := sb.run(nil, "--in-tree")
	sb.assertExit(r, 0)
	sb.assertInstalled(string(payload))
	sb.assertBinEntries("usagebar", legacyName, ownerOnlyName, unscopedName, noncanonicalName, fileName, linkName)
	for _, path := range []string{legacy, ownerOnly, unscoped, noncanonical, file, keep} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("entry was removed: %v", err)
		}
	}
}

// TestEnsureBinary_UnavailableScopeNeverReclaims checks that without a
// well-formed boot identity nothing is reclaimed, even a stage whose owner is
// dead in the scope it would have had. The install stages under the unscoped
// name, which is never reclaimed.
func TestEnsureBinary_UnavailableScopeNeverReclaims(t *testing.T) {
	o := ensureReleaseOpts()
	o.bootID = ""
	sb := newEnsureSandbox(t, o)
	dead := stageDirName(ensureTestScope, 99999999, "ABCDEF")
	partial := sb.seedStage(dead)
	payload := ensurePayload("release", 0)
	sb.publish(ensureTestTag, ensureTestAsset, payload)
	sb.publish(ensureTestTag, "SHA256SUMS", ensureSums(payload, "  "))

	r := sb.run(nil, "--in-tree")
	sb.assertExit(r, 0)
	sb.assertInstalled(string(payload))
	sb.assertBinEntries("usagebar", dead)
	if _, err := os.Stat(partial); err != nil {
		t.Fatalf("stage was reclaimed without a scope: %v", err)
	}
	if !strings.Contains(r.out, "cannot identify this host's boot and PID namespace") {
		t.Fatalf("output does not report the unavailable scope:\n%s", r.out)
	}
	unscoped := false
	for _, line := range sb.requestLines(sb.ghLog) {
		if strings.Contains(line, "/.usagebar-download.unscoped.") {
			unscoped = true
		}
	}
	if !unscoped {
		t.Fatalf("download did not stage under the unscoped name; gh log:\n%q", sb.requestLines(sb.ghLog))
	}
}

// TestEnsureBinary_MalformedScopeIdentityNeverReclaims checks that a boot ID,
// PID namespace, or session UUID that is not well-formed leaves the scope
// unavailable, so nothing is reclaimed. The stage name in each case is the
// scope a reader of the bad value would have built.
func TestEnsureBinary_MalformedScopeIdentityNeverReclaims(t *testing.T) {
	cases := []struct {
		name  string
		edit  func(*ensureOpts)
		asset string
		scope string
	}{
		{"boot_id not a UUID", func(o *ensureOpts) { o.bootID = "not-a-uuid" }, ensureTestAsset, "linux-not-a-uuid-pidns4026531836"},
		{"boot_id with trailing junk", func(o *ensureOpts) { o.bootID = ensureTestBootID + "x" }, ensureTestAsset, "linux-" + ensureTestBootID + "x-pidns4026531836"},
		{"pid namespace without pid:[] form", func(o *ensureOpts) { o.pidNS = "4026531836" }, ensureTestAsset, ensureTestScope},
		{"pid namespace unavailable", func(o *ensureOpts) { o.pidNS = "" }, ensureTestAsset, ensureTestScope},
		{"darwin session not a UUID", func(o *ensureOpts) {
			o.unameS = "Darwin"
			o.session = "not-a-uuid"
		}, "usagebar-darwin-amd64", "darwin-not-a-uuid"},
		{"darwin session unavailable", func(o *ensureOpts) {
			o.unameS = "Darwin"
			o.session = ""
		}, "usagebar-darwin-amd64", ensureTestDarwinScope},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o := ensureReleaseOpts()
			tc.edit(&o)
			sb := newEnsureSandbox(t, o)
			dead := stageDirName(tc.scope, 99999999, "ABCDEF")
			partial := sb.seedStage(dead)
			payload := ensurePayload("release", 0)
			sb.publish(ensureTestTag, tc.asset, payload)
			sb.publish(ensureTestTag, "SHA256SUMS", ensureSumsFor(tc.asset, payload, "  "))

			r := sb.run(nil, "--in-tree")
			sb.assertExit(r, 0)
			sb.assertInstalled(string(payload))
			sb.assertBinEntries("usagebar", dead)
			if _, err := os.Stat(partial); err != nil {
				t.Fatalf("stage %s was reclaimed: %v", dead, err)
			}
		})
	}
}

// TestEnsureBinary_KeepsStagesWhenProcessListingFails checks that cleanup never
// blocks the install: with ps broken, a dead owner's directory is kept.
func TestEnsureBinary_KeepsStagesWhenProcessListingFails(t *testing.T) {
	o := ensureReleaseOpts()
	o.psBroken = true
	sb := newEnsureSandbox(t, o)
	dead := stageDirName(ensureTestScope, 99999999, "ABCDEF")
	sb.seedStage(dead)
	payload := ensurePayload("release", 0)
	sb.publish(ensureTestTag, ensureTestAsset, payload)
	sb.publish(ensureTestTag, "SHA256SUMS", ensureSums(payload, "  "))

	r := sb.run(nil, "--in-tree")
	sb.assertExit(r, 0)
	sb.assertInstalled(string(payload))
	sb.assertBinEntries("usagebar", dead)
}

// TestEnsureBinary_KeepsStageWhenListingOmitsInstaller checks that a process
// listing which succeeds but does not contain the installer's own PID is not
// trusted, so a dead owner's stage is kept on both Linux and Darwin. The listing
// names only PID 1, so the owner is absent from it too; only the installer-PID
// check separates this case from a reclaim.
func TestEnsureBinary_KeepsStageWhenListingOmitsInstaller(t *testing.T) {
	cases := []struct {
		name   string
		unameS string
		asset  string
		scope  string
	}{
		{"linux", "", ensureTestAsset, ensureTestScope},
		{"darwin", "Darwin", "usagebar-darwin-amd64", ensureTestDarwinScope},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o := ensureReleaseOpts()
			o.unameS = tc.unameS
			o.psListing = "1\n"
			sb := newEnsureSandbox(t, o)
			dead := stageDirName(tc.scope, 99999999, "ABCDEF")
			partial := sb.seedStage(dead)
			payload := ensurePayload("release", 0)
			sb.publish(ensureTestTag, tc.asset, payload)
			sb.publish(ensureTestTag, "SHA256SUMS", ensureSumsFor(tc.asset, payload, "  "))

			r := sb.run(nil, "--in-tree")
			sb.assertExit(r, 0)
			sb.assertInstalled(string(payload))
			sb.assertBinEntries("usagebar", dead)
			if _, err := os.Stat(partial); err != nil {
				t.Fatalf("stage %s was reclaimed: %v", dead, err)
			}
		})
	}
}

// TestEnsureBinary_ProcViewGatesStageReclaim checks that a dead owner's stage is
// reclaimed only when /proc shows the installer's own PID namespace. The owner is
// absent from the process listing while the installer's PID is present in it, the
// combination a bind-mounted /proc from another namespace produces. Every fault
// leaves the stage in place and the install still succeeds.
func TestEnsureBinary_ProcViewGatesStageReclaim(t *testing.T) {
	cases := []struct {
		name      string
		procView  string
		reclaimed bool
	}{
		{"matching view reclaims", "", true},
		{"nested namespace view reclaims", "nested-valid", true},
		{"caller namespace differs", "ns-differs", false},
		{"caller namespace unreadable", "ns-unreadable", false},
		{"status unreadable", "status-unreadable", false},
		{"status without NSpid", "status-no-nspid", false},
		{"status with two NSpid lines", "status-two-nspid", false},
		{"innermost NSpid is not the installer PID", "nspid-mismatch", false},
		{"malformed NSpid", "nspid-malformed", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o := ensureReleaseOpts()
			o.procView = tc.procView
			sb := newEnsureSandbox(t, o)
			dead := stageDirName(ensureTestScope, 99999999, "ABCDEF")
			partial := sb.seedStage(dead)
			payload := ensurePayload("release", 0)
			sb.publish(ensureTestTag, ensureTestAsset, payload)
			sb.publish(ensureTestTag, "SHA256SUMS", ensureSums(payload, "  "))

			r := sb.run(nil, "--in-tree")
			sb.assertExit(r, 0)
			sb.assertInstalled(string(payload))
			if tc.reclaimed {
				sb.assertBinEntries("usagebar")
				return
			}
			sb.assertBinEntries("usagebar", dead)
			if _, err := os.Stat(partial); err != nil {
				t.Fatalf("stage was reclaimed without a verified PID namespace: %v", err)
			}
		})
	}
}

// ensureStageName matches a scoped staging directory name and captures its
// scope and owner PID.
var ensureStageName = regexp.MustCompile(`^\.usagebar-download\.([a-z0-9-]+)\.([1-9][0-9]*)\.[A-Za-z0-9]{6}$`)

// stageOwnerPID returns the owner PID encoded in a scoped staging directory
// name of this sandbox's scope.
func stageOwnerPID(t *testing.T, stage string) int {
	t.Helper()
	m := ensureStageName.FindStringSubmatch(stage)
	if m == nil {
		t.Fatalf("%q is not a scoped staging directory", stage)
	}
	if m[1] != ensureTestScope {
		t.Fatalf("%q has scope %q, want %q", stage, m[1], ensureTestScope)
	}
	pid, err := strconv.Atoi(m[2])
	if err != nil {
		t.Fatalf("parse owner PID of %q: %v", stage, err)
	}
	return pid
}

// TestEnsureBinary_StagesAreOwnedByStagingSubshell checks that a staging
// directory is named after the subshell that downloads into it, not after the
// installer shell that spawned that subshell. The payload runs directly from
// the subshell, so its PPID is the owner the directory name must carry.
func TestEnsureBinary_StagesAreOwnedByStagingSubshell(t *testing.T) {
	sb := newEnsureSandbox(t, ensureReleaseOpts())
	payload := []byte("#!/bin/sh\nd=\"${0%/*}\"\nprintf '%s %s\\n' \"$PPID\" \"${d##*/}\" >> \"$ENSURE_TEST_MARKER\"\nexit 0\n")
	sb.publish(ensureTestTag, ensureTestAsset, payload)
	sb.publish(ensureTestTag, "SHA256SUMS", ensureSums(payload, "  "))

	r := sb.run(nil, "--in-tree")
	sb.assertExit(r, 0)
	sb.assertInstalled(string(payload))
	data, err := os.ReadFile(sb.marker)
	if err != nil {
		t.Fatalf("payload never ran: %v", err)
	}
	fields := strings.Fields(string(data))
	if len(fields) != 2 {
		t.Fatalf("payload marker = %q, want \"<ppid> <stage>\"", data)
	}
	ppid, err := strconv.Atoi(fields[0])
	if err != nil {
		t.Fatalf("payload PPID %q: %v", fields[0], err)
	}
	if ppid == r.pid {
		t.Fatalf("payload ran under installer shell %d; it must run under the staging subshell", r.pid)
	}
	if owner := stageOwnerPID(t, fields[1]); owner != ppid {
		t.Fatalf("staging directory %s is owned by PID %d, but the staging subshell is %d", fields[1], owner, ppid)
	}
	sb.assertBinEntries("usagebar")
}

// startInstaller runs the entrypoint in the background without waiting for it.
// Output goes to a file, so a killed installer's orphaned children cannot hold
// a pipe open and block Wait.
func (sb *ensureSandbox) startInstaller(args ...string) *exec.Cmd {
	sb.t.Helper()
	logFile, err := os.Create(filepath.Join(filepath.Dir(sb.repo), "installer.log"))
	if err != nil {
		sb.t.Fatalf("create installer log: %v", err)
	}
	sb.t.Cleanup(func() { _ = logFile.Close() })
	script := filepath.Join(sb.repo, "bin", "ensure-binary.sh")
	cmd := exec.Command(sb.bash, append([]string{script}, args...)...)
	cmd.Dir = sb.repo
	cmd.Env = []string{"PATH=" + sb.pathDir, "ENSURE_TEST_MARKER=" + sb.marker}
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		sb.t.Fatalf("start ensure-binary: %v", err)
	}
	return cmd
}

// soleStage returns the single staging directory in bin/, failing otherwise.
func (sb *ensureSandbox) soleStage() string {
	sb.t.Helper()
	entries, err := os.ReadDir(filepath.Join(sb.repo, "bin"))
	if err != nil {
		sb.t.Fatalf("read bin/: %v", err)
	}
	var stages []string
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".usagebar-download.") {
			stages = append(stages, entry.Name())
		}
	}
	if len(stages) != 1 {
		sb.t.Fatalf("staging directories = %v, want exactly one", stages)
	}
	return stages[0]
}

// ensurePathExists reports whether path exists without following symlinks.
func ensurePathExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// waitFor polls cond until it holds, failing the test after ensureWaitTimeout.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(ensureWaitTimeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestEnsureBinary_KillingInstallerShellKeepsStageOfLiveSubshell covers the
// window the owner must survive: the installer shell is killed while its
// staging subshell is still downloading. A parallel installer must keep that
// stage, since its owner is alive, and the held download must still finish.
func TestEnsureBinary_KillingInstallerShellKeepsStageOfLiveSubshell(t *testing.T) {
	o := ensureReleaseOpts()
	o.holdGh = true
	sb := newEnsureSandbox(t, o)
	payload := ensurePayload("release", 0)
	sb.publish(ensureTestTag, ensureTestAsset, payload)
	sb.publish(ensureTestTag, "SHA256SUMS", ensureSums(payload, "  "))
	release := filepath.Join(sb.hold, "release")
	t.Cleanup(func() { _ = os.WriteFile(release, nil, 0o644) })

	first := sb.startInstaller("--in-tree")
	waitFor(t, "the staging subshell to reach its download", func() bool {
		return ensurePathExists(filepath.Join(sb.hold, "ready"))
	})
	stage := sb.soleStage()
	owner := stageOwnerPID(t, stage)
	if owner == first.Process.Pid {
		t.Fatalf("stage %s carries installer shell PID %d, not the staging subshell", stage, owner)
	}
	// Kill only the installer shell. Its staging subshell stays blocked in the
	// held download.
	if err := first.Process.Kill(); err != nil {
		t.Fatalf("kill installer shell: %v", err)
	}
	_ = first.Wait()
	if err := syscall.Kill(owner, 0); err != nil {
		t.Fatalf("staging subshell %d is not alive after its installer was killed: %v", owner, err)
	}

	second := sb.run(nil, "--in-tree")
	sb.assertExit(second, 0)
	if !ensurePathExists(filepath.Join(sb.repo, "bin", stage)) {
		t.Fatalf("parallel installer removed the live stage %s", stage)
	}

	ensureWriteFile(t, release, nil, 0o644)
	waitFor(t, "the staging subshell to finish", func() bool {
		return !ensurePathExists(filepath.Join(sb.repo, "bin", stage))
	})
	sb.assertInstalled(string(payload))
	sb.assertBinEntries("usagebar")
}

// TestEnsureBinary_InstallsWhenTMPDIRMissing checks that an unusable TMPDIR does
// not block the install: the owner probe falls back to /tmp, so a verified
// binary is still installed from the exact release.
func TestEnsureBinary_InstallsWhenTMPDIRMissing(t *testing.T) {
	sb := newEnsureSandbox(t, ensureReleaseOpts())
	payload := ensurePayload("release", 0)
	sb.publish(ensureTestTag, ensureTestAsset, payload)
	sb.publish(ensureTestTag, "SHA256SUMS", ensureSums(payload, "  "))

	r := sb.run([]string{"TMPDIR=" + filepath.Join(sb.repo, "no-such-tmp")}, "--in-tree")
	sb.assertExit(r, 0)
	sb.assertInstalled(string(payload))
	sb.assertSanityRunOnly()
	sb.assertBinEntries("usagebar")
	sb.assertExactTagRequests()
}

// TestEnsureBinary_FailsClosedWhenStageOwnerUnknown covers an owner PID that
// cannot be recorded: the probe directory cannot be created under TMPDIR, /tmp,
// or bin/, so the install refuses before any staging directory or release
// request exists. Staging mktemp still works, so only the owner probe is broken.
func TestEnsureBinary_FailsClosedWhenStageOwnerUnknown(t *testing.T) {
	sb := newEnsureSandbox(t, ensureReleaseOpts())
	sb.breakOwnerProbes()
	payload := ensurePayload("release", 0)
	sb.publish(ensureTestTag, ensureTestAsset, payload)
	sb.publish(ensureTestTag, "SHA256SUMS", ensureSums(payload, "  "))

	r := sb.run([]string{"TMPDIR=" + filepath.Join(sb.repo, "no-such-tmp")}, "--in-tree")
	sb.assertExit(r, 127)
	if !strings.Contains(r.out, "staging subshell") {
		t.Fatalf("output does not report the unknown owner:\n%s", r.out)
	}
	sb.assertNotExecuted()
	sb.assertNoRequests()
	sb.assertBinEntries()
}

// breakOwnerProbes replaces the sandbox mktemp with a shim that fails only for
// owner probe directories (usagebar-owner.*), delegating every other call to
// the host mktemp so staging still works.
func (sb *ensureSandbox) breakOwnerProbes() {
	sb.t.Helper()
	real, err := exec.LookPath("mktemp")
	if err != nil {
		sb.t.Fatalf("host mktemp: %v", err)
	}
	if err := os.Remove(filepath.Join(sb.pathDir, "mktemp")); err != nil {
		sb.t.Fatalf("unlink mktemp: %v", err)
	}
	sb.writeExecutable("mktemp", "#!/bin/sh\ncase \"$*\" in\n  *usagebar-owner.*) exit 1 ;;\nesac\nexec "+ensureShQuote(real)+" \"$@\"\n")
}
