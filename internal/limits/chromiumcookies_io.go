/**
 * Locates Chromium-family cookie databases on macOS and reads one domain's
 * rows, decrypting them with the browser's Keychain "<Browser> Safe Storage"
 * password. Pure decryption/matching helpers live in chromiumcookies.go.
 *
 * The Keychain is only consulted after a profile actually yields rows for the
 * domain, so browsers without that session never trigger an access prompt.
 * Set USAGEBAR_DISABLE_BROWSER_COOKIES=1 to opt out entirely.
 */
package limits

import (
	"context"
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// chromiumBrowser is one installed Chromium-family browser.
type chromiumBrowser struct {
	Name string
	// SupportDir is relative to ~/Library/Application Support.
	SupportDir      string
	KeychainService string
	KeychainAccount string
}

// chromiumBrowsers are probed in this order; the first profile that yields a
// usable session for the domain wins.
var chromiumBrowsers = []chromiumBrowser{
	{"Google Chrome", "Google/Chrome", "Chrome Safe Storage", "Chrome"},
	{"Arc", "Arc/User Data", "Arc Safe Storage", "Arc"},
	{"Brave", "BraveSoftware/Brave-Browser", "Brave Safe Storage", "Brave"},
	{"Microsoft Edge", "Microsoft Edge", "Microsoft Edge Safe Storage", "Microsoft Edge"},
	{"Chromium", "Chromium", "Chromium Safe Storage", "Chromium"},
}

// ChromiumCookieStore is one browser profile's cookie DB plus the Keychain
// entry holding its encryption password.
type ChromiumCookieStore struct {
	Browser         string
	Profile         string
	DBPath          string
	KeychainService string
	KeychainAccount string
}

// chromiumCookieRow is one raw row before decryption.
type chromiumCookieRow struct {
	HostKey        string
	Name           string
	Value          string
	EncryptedValue []byte
	ExpiresUTC     int64
}

func chromiumSupportRoot() string {
	return filepath.Join(userHome(), "Library", "Application Support")
}

// chromiumProfileDirs lists a browser's profile directories, "Default" first.
func chromiumProfileDirs(browserRoot string) []string {
	entries, err := os.ReadDir(browserRoot)
	if err != nil {
		return nil
	}
	var profiles []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if name != "Default" && !strings.HasPrefix(name, "Profile ") {
			continue
		}
		profiles = append(profiles, name)
	}
	sort.Slice(profiles, func(i, j int) bool {
		if (profiles[i] == "Default") != (profiles[j] == "Default") {
			return profiles[i] == "Default"
		}
		return profiles[i] < profiles[j]
	})
	return profiles
}

// ChromiumCookieStores returns every existing cookie DB on this machine.
// Chromium moved the file to <profile>/Network/Cookies; the older
// <profile>/Cookies location is still checked for long-lived installs.
func ChromiumCookieStores() []ChromiumCookieStore {
	root := chromiumSupportRoot()
	var stores []ChromiumCookieStore
	for _, b := range chromiumBrowsers {
		browserRoot := filepath.Join(root, filepath.FromSlash(b.SupportDir))
		for _, profile := range chromiumProfileDirs(browserRoot) {
			for _, rel := range [][]string{{"Network", "Cookies"}, {"Cookies"}} {
				path := filepath.Join(append([]string{browserRoot, profile}, rel...)...)
				if st, err := os.Stat(path); err != nil || !st.Mode().IsRegular() {
					continue
				}
				stores = append(stores, ChromiumCookieStore{
					Browser:         b.Name,
					Profile:         profile,
					DBPath:          path,
					KeychainService: b.KeychainService,
					KeychainAccount: b.KeychainAccount,
				})
				break
			}
		}
	}
	return stores
}

// readChromiumCookieRows reads unexpired rows for domain. immutable=1 is
// required because a running browser holds the DB with exclusive locking;
// it also guarantees this never writes to the browser's file.
func readChromiumCookieRows(dbPath, domain string, nowMs int64) ([]chromiumCookieRow, error) {
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro&immutable=1")
	if err != nil {
		return nil, err
	}
	defer db.Close()

	// Longest host_key first so a host-exact cookie wins over a domain-wide
	// duplicate of the same name in BuildCookieHeader.
	rows, err := db.Query(`
		SELECT host_key, name, value, encrypted_value, expires_utc
		FROM cookies
		WHERE host_key LIKE ?
		ORDER BY LENGTH(host_key) DESC, name`, "%"+domain)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []chromiumCookieRow
	for rows.Next() {
		var r chromiumCookieRow
		if err := rows.Scan(&r.HostKey, &r.Name, &r.Value, &r.EncryptedValue, &r.ExpiresUTC); err != nil {
			continue
		}
		if !CookieHostMatchesDomain(r.HostKey, domain) {
			continue
		}
		if ChromiumCookieExpired(r.ExpiresUTC, nowMs) {
			continue
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// chromiumSafeStoragePassword reads the browser's encryption password from the
// login Keychain. The first read shows a macOS access prompt; granting it once
// ("Always Allow") keeps later refreshes silent.
func chromiumSafeStoragePassword(service, account string) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "security", "find-generic-password", "-w", "-s", service, "-a", account).Output()
	if err != nil {
		return "", false
	}
	password := strings.TrimRight(string(out), "\r\n")
	if password == "" {
		return "", false
	}
	return password, true
}

// BrowserCookieImport is the result of a successful import, kept separate from
// the header so callers can report which profile supplied the session without
// logging the values.
type BrowserCookieImport struct {
	Header  string
	Browser string
	Profile string
	Names   []string
}

// BrowserCookieProbe records what happened for one profile, so a failed
// import can be attributed to the profile having no session, the Keychain
// lookup being refused, or the values not decrypting. Diagnostics only — it
// never carries a cookie value.
type BrowserCookieProbe struct {
	Browser    string
	Profile    string
	Rows       int
	KeychainOK bool
	Decrypted  int
}

// ImportBrowserCookieHeader builds a Cookie header for domain from the first
// local Chromium profile that has a usable session for it.
func ImportBrowserCookieHeader(domain string, nowMs int64) (BrowserCookieImport, bool) {
	imported, _, ok := ImportBrowserCookieHeaderWithProbes(domain, nowMs)
	return imported, ok
}

// ImportBrowserCookieHeaderWithProbes is ImportBrowserCookieHeader plus a
// per-profile trace for the opencode-check diagnostic.
func ImportBrowserCookieHeaderWithProbes(domain string, nowMs int64) (BrowserCookieImport, []BrowserCookieProbe, bool) {
	if strings.TrimSpace(os.Getenv("USAGEBAR_DISABLE_BROWSER_COOKIES")) != "" {
		return BrowserCookieImport{}, nil, false
	}
	var probes []BrowserCookieProbe
	for _, store := range ChromiumCookieStores() {
		probe := BrowserCookieProbe{Browser: store.Browser, Profile: store.Profile}
		rows, err := readChromiumCookieRows(store.DBPath, domain, nowMs)
		probe.Rows = len(rows)
		if err != nil || len(rows) == 0 {
			probes = append(probes, probe)
			continue
		}
		password, ok := chromiumSafeStoragePassword(store.KeychainService, store.KeychainAccount)
		probe.KeychainOK = ok
		if !ok {
			probes = append(probes, probe)
			continue
		}
		key, err := DeriveChromiumKey(password)
		if err != nil {
			probes = append(probes, probe)
			continue
		}
		var cookies []BrowserCookie
		for _, r := range rows {
			value, ok := DecryptChromiumCookieValue(key, r.HostKey, r.EncryptedValue)
			if !ok {
				// Rows written before encryption was available keep a plain value.
				value = r.Value
			}
			if value == "" {
				continue
			}
			cookies = append(cookies, BrowserCookie{HostKey: r.HostKey, Name: r.Name, Value: value})
		}
		probe.Decrypted = len(cookies)
		probes = append(probes, probe)
		header := BuildCookieHeader(cookies)
		if header == "" {
			continue
		}
		names := make([]string, 0, len(cookies))
		for _, c := range cookies {
			names = append(names, c.Name)
		}
		return BrowserCookieImport{
			Header:  header,
			Browser: store.Browser,
			Profile: store.Profile,
			Names:   names,
		}, probes, true
	}
	return BrowserCookieImport{}, probes, false
}
