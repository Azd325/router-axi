package tr064

import (
	"io/fs"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var (
	ipv4Candidate = regexp.MustCompile(`\b\d{1,3}(?:\.\d{1,3}){3}\b`)
	ipv6Candidate = regexp.MustCompile(`[0-9A-Fa-f:.]*:[0-9A-Fa-f:.]*:[0-9A-Fa-f:.]*`)
	cgnat         = netip.PrefixFrom(netip.AddrFrom4([4]byte{100, 64, 0, 0}), 10)
)

func isRouterAddress(candidate string) bool {
	address, err := netip.ParseAddr(candidate)
	if err != nil {
		return false
	}
	address = address.Unmap()
	return address.IsPrivate() || address.IsLinkLocalUnicast() || cgnat.Contains(address)
}

func privateAddressesIn(text string) []string {
	var found []string
	for _, pattern := range []*regexp.Regexp{ipv4Candidate, ipv6Candidate} {
		for _, candidate := range pattern.FindAllString(text, -1) {
			if isRouterAddress(strings.Trim(candidate, ":.")) || isRouterAddress(candidate) {
				found = append(found, candidate)
			}
		}
	}
	return found
}

func dotted(octets ...string) string { return strings.Join(octets, ".") }

func TestRouterAddressClassification(t *testing.T) {
	for candidate, want := range map[string]bool{
		dotted("192", "168", "178", "1"):           true,
		dotted("192", "168", "0", "1"):             true,
		dotted("10", "0", "0", "1"):                true,
		dotted("172", "16", "0", "1"):              true,
		dotted("172", "31", "255", "255"):          true,
		dotted("169", "254", "1", "1"):             true,
		dotted("100", "64", "0", "1"):              true,
		dotted("100", "127", "255", "254"):         true,
		"fd00" + "::1":                             true,
		"fe80" + "::1":                             true,
		"::ffff:" + dotted("192", "168", "1", "1"): true,
		dotted("192", "0", "2", "10"):              false,
		dotted("198", "51", "100", "7"):            false,
		dotted("203", "0", "113", "9"):             false,
		dotted("127", "0", "0", "1"):               false,
		dotted("172", "32", "0", "1"):              false,
		dotted("100", "128", "0", "1"):             false,
		"2001:db8::1":                              false,
		"::1":                                      false,
		"02:00:00:00:00:01":                        false,
	} {
		if got := len(privateAddressesIn(candidate)) > 0; got != want {
			t.Errorf("privateAddressesIn(%q) found=%t, want %t", candidate, got, want)
		}
	}
}

func TestRepositoryFilesContainNoRouterAddresses(t *testing.T) {
	root := filepath.Join("..", "..")
	scanned := 0
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "bin":
				return filepath.SkipDir
			}
			return nil
		}
		if !coveredByAddressScan(filepath.ToSlash(relative)) {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		scanned++
		for number, line := range strings.Split(string(data), "\n") {
			for _, address := range privateAddressesIn(line) {
				t.Errorf("%s:%d contains private, link-local, CGNAT or factory-default address %q; use a documentation address (192.0.2.0/24, 198.51.100.0/24, 203.0.113.0/24, 2001:db8::/32)", relative, number+1, address)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if scanned < 20 {
		t.Fatalf("scanned %d files, want the repository fixtures, tests and docs", scanned)
	}
}

func coveredByAddressScan(path string) bool {
	switch path {
	case "README.md", "VISION.md", "internal/skill/SKILL.md":
		return true
	}
	return strings.HasSuffix(path, "_test.go") || strings.Contains("/"+path, "/testdata/")
}
