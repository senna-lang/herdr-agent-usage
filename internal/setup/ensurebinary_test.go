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
	"sort"
	"strings"
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
)

// ensureBaseTools are the only host utilities the entrypoint may use. PATH is
// otherwise the sandbox alone, so no host gh, go, usagebar, or digest tool
// leaks into a case.
var ensureBaseTools = []string{"awk", "chmod", "cp", "dirname", "mkdir", "mktemp", "mv", "rm", "tr"}

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
}

func ensureReleaseOpts() ensureOpts {
	return ensureOpts{
		version:   ensureTestVersion,
		gh:        true,
		curl:      true,
		verifiers: []string{"sha256sum"},
	}
}

type ensureRun struct {
	code int
	out  string
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
	}
	ensureMkdir(t, filepath.Join(sb.repo, "bin"))
	ensureMkdir(t, sb.pathDir)
	ensureMkdir(t, sb.store)
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
	for _, name := range o.verifiers {
		sb.linkVerifier(name, o.brokenSHA)
	}
	if o.gh {
		sb.writeGhShim(o.ghBroken)
	}
	if o.curl {
		sb.writeCurlShim()
	}
	if o.goShim {
		sb.writeGoShim(o.goFail)
	}
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
// from the release store and logs every invocation.
func (sb *ensureSandbox) writeGhShim(broken bool) {
	body := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + ensureShQuote(sb.ghLog) + "\n"
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
		return ensureRun{code: 0, out: string(out)}
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		sb.t.Fatalf("run ensure-binary: %v", err)
	}
	return ensureRun{code: exitErr.ExitCode(), out: string(out)}
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
	return []byte(ensureSha256Hex(payload) + sep + ensureTestAsset + "\n")
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
