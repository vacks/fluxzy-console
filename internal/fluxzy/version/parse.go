package version

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var versionPattern = regexp.MustCompile(`(?i)(?:fluxzy\s+)?v?([0-9]+)\.([0-9]+)\.([0-9]+)(?:\.([0-9]+))?(?:\+([0-9A-Za-z._-]+))?`)

// Info is a fluxzy CLI or release version.
type Info struct {
	Raw        string
	Major      int
	Minor      int
	Patch      int
	Build      int
	Commit     string
	ReleaseTag string
}

func (v Info) Base() string {
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
}

func (v Info) String() string {
	if v.ReleaseTag != "" {
		return v.ReleaseTag
	}
	if v.Commit != "" {
		return v.Base() + "+" + v.Commit
	}
	return v.Base()
}

func (v Info) IsZero() bool { return v.Raw == "" && v.Major == 0 && v.Minor == 0 && v.Patch == 0 }

// ParseCLIOutput parses `fluxzy --version` output. Current releases print
// values such as "1.39.51+f4a52d1e00"; older/normalized values may contain
// four numeric components.
func ParseCLIOutput(output string) (Info, error) {
	m := versionPattern.FindStringSubmatch(output)
	if m == nil {
		return Info{}, fmt.Errorf("unrecognized fluxzy version output %q", strings.TrimSpace(output))
	}
	major, _ := strconv.Atoi(m[1])
	minor, _ := strconv.Atoi(m[2])
	patch, _ := strconv.Atoi(m[3])
	build := 0
	if m[4] != "" {
		build, _ = strconv.Atoi(m[4])
	}
	return Info{
		Raw:    strings.TrimSpace(output),
		Major:  major,
		Minor:  minor,
		Patch:  patch,
		Build:  build,
		Commit: m[5],
	}, nil
}

// ParseReleaseTag parses a GitHub release tag like v1.39.51.62629.
func ParseReleaseTag(tag string) (Info, error) {
	raw := strings.TrimSpace(tag)
	clean := strings.TrimPrefix(raw, "v")
	parts := strings.Split(clean, ".")
	if len(parts) < 3 || len(parts) > 4 {
		return Info{}, fmt.Errorf("invalid fluxzy release tag %q", tag)
	}
	nums := make([]int, len(parts))
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return Info{}, fmt.Errorf("invalid fluxzy release tag %q", tag)
		}
		nums[i] = n
	}
	build := 0
	if len(nums) == 4 {
		build = nums[3]
	}
	return Info{
		Raw:        raw,
		Major:      nums[0],
		Minor:      nums[1],
		Patch:      nums[2],
		Build:      build,
		ReleaseTag: clean,
	}, nil
}

// CompareBase compares only the three-component base version. It is used when
// the running CLI does not expose a release/build component, so the code cannot
// know whether it is older than the same base release with an explicit build.
func CompareBase(a, b Info) int {
	for _, pair := range [][2]int{
		{a.Major, b.Major},
		{a.Minor, b.Minor},
		{a.Patch, b.Patch},
	} {
		if pair[0] < pair[1] {
			return -1
		}
		if pair[0] > pair[1] {
			return 1
		}
	}
	return 0
}

// Compare compares the numeric release tuple. A missing build component sorts
// below the same base with an explicit build.
func Compare(a, b Info) int {
	for _, pair := range [][2]int{
		{a.Major, b.Major},
		{a.Minor, b.Minor},
		{a.Patch, b.Patch},
		{a.Build, b.Build},
	} {
		if pair[0] < pair[1] {
			return -1
		}
		if pair[0] > pair[1] {
			return 1
		}
	}
	return 0
}

func CompareRelease(a, b string) (int, error) {
	ai, err := ParseReleaseTag(a)
	if err != nil {
		return 0, err
	}
	bi, err := ParseReleaseTag(b)
	if err != nil {
		return 0, err
	}
	return Compare(ai, bi), nil
}
