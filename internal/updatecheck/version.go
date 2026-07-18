// Package updatecheck checks whether a newer plugin release is available.
package updatecheck

import (
	"fmt"
	"strconv"
	"strings"
)

// Version is a semantic release version without build metadata.
type Version struct {
	Major int
	Minor int
	Patch int
}

// ParseVersion accepts a release tag such as v0.1.1 or 0.1.1. Prereleases
// and development versions are deliberately not comparable to releases.
func ParseVersion(raw string) (Version, error) {
	raw = strings.TrimPrefix(strings.TrimSpace(raw), "v")
	if strings.ContainsAny(raw, "+-") {
		return Version{}, fmt.Errorf("not a stable release version: %q", raw)
	}
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return Version{}, fmt.Errorf("invalid release version: %q", raw)
	}
	values := [3]int{}
	for i, part := range parts {
		if part == "" || (len(part) > 1 && part[0] == '0') {
			return Version{}, fmt.Errorf("invalid release version: %q", raw)
		}
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return Version{}, fmt.Errorf("invalid release version: %q", raw)
		}
		values[i] = n
	}
	return Version{Major: values[0], Minor: values[1], Patch: values[2]}, nil
}

func (v Version) String() string {
	return fmt.Sprintf("v%d.%d.%d", v.Major, v.Minor, v.Patch)
}

// CompareVersions returns -1, 0, or 1 when a is smaller than, equal to, or
// greater than b respectively.
func CompareVersions(a, b Version) int {
	if a.Major != b.Major {
		return compareInt(a.Major, b.Major)
	}
	if a.Minor != b.Minor {
		return compareInt(a.Minor, b.Minor)
	}
	return compareInt(a.Patch, b.Patch)
}

func compareInt(a, b int) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}
