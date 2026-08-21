// Package zbx holds Zabbix version handling and the capability checks built on
// it. Capabilities are asserted explicitly rather than probed, so an
// incompatibility surfaces as a clear error instead of an empty result.
package zbx

import (
	"fmt"
	"strconv"
	"strings"
)

// Version is a parsed Zabbix API version.
type Version struct {
	Major int
	Minor int
	Patch int
	Raw   string
}

// ParseVersion reads a version as reported by apiinfo.version, for example
// "7.4.10". Trailing qualifiers such as "7.0.0rc1" are tolerated.
func ParseVersion(s string) (Version, error) {
	v := Version{Raw: strings.TrimSpace(s)}
	parts := strings.SplitN(v.Raw, ".", 3)
	if len(parts) < 2 {
		return v, fmt.Errorf("unrecognised Zabbix version %q", s)
	}
	var err error
	if v.Major, err = strconv.Atoi(parts[0]); err != nil {
		return v, fmt.Errorf("unrecognised Zabbix version %q", s)
	}
	if v.Minor, err = strconv.Atoi(parts[1]); err != nil {
		return v, fmt.Errorf("unrecognised Zabbix version %q", s)
	}
	if len(parts) == 3 {
		digits := parts[2]
		for i, r := range digits {
			if r < '0' || r > '9' {
				digits = digits[:i]
				break
			}
		}
		v.Patch, _ = strconv.Atoi(digits)
	}
	return v, nil
}

// AtLeast reports whether v is at least major.minor.
func (v Version) AtLeast(major, minor int) bool {
	if v.Major != major {
		return v.Major > major
	}
	return v.Minor >= minor
}

func (v Version) String() string {
	if v.Raw != "" {
		return v.Raw
	}
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
}

// MinimumSupported is the oldest Zabbix release this program targets. Bearer
// token authentication arrived in 6.4 and is the only scheme implemented here.
var MinimumSupported = Version{Major: 6, Minor: 4}

// Capability names a feature whose availability depends on the server version.
type Capability string

const (
	// CapBearerAuth is header-based API token authentication (6.4+). Older
	// servers expect the token in the request's auth parameter.
	CapBearerAuth Capability = "bearer_auth"
	// CapActiveAvailable is host.active_available, the availability of the
	// active agent, added in 6.4.
	CapActiveAvailable Capability = "active_available"
	// CapHostGroupSelect is maintenance.get's selectHostGroups, renamed from
	// selectGroups in 6.0.
	CapHostGroupSelect Capability = "select_host_groups"
	// CapCauseEventID is problem.get's cause_eventid, which distinguishes a
	// cause from its symptoms, added in 6.4.
	CapCauseEventID Capability = "cause_eventid"
)

var capabilityMinimum = map[Capability][2]int{
	CapBearerAuth:      {6, 4},
	CapActiveAvailable: {6, 4},
	CapHostGroupSelect: {6, 0},
	CapCauseEventID:    {6, 4},
}

// Supports reports whether the server offers the capability.
func (v Version) Supports(c Capability) bool {
	min, ok := capabilityMinimum[c]
	if !ok {
		return true
	}
	return v.AtLeast(min[0], min[1])
}

// Requirement describes the version a capability needs, for error messages.
func Requirement(c Capability) string {
	min, ok := capabilityMinimum[c]
	if !ok {
		return "any supported version"
	}
	return fmt.Sprintf("%d.%d or newer", min[0], min[1])
}
