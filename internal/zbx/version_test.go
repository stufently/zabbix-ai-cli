package zbx

import "testing"

func TestParseVersion(t *testing.T) {
	for _, tc := range []struct {
		in                  string
		major, minor, patch int
		wantErr             bool
	}{
		{in: "7.4.10", major: 7, minor: 4, patch: 10},
		{in: "6.0.0", major: 6, minor: 0},
		{in: "7.0", major: 7, minor: 0},
		{in: "7.0.0rc1", major: 7, minor: 0},
		{in: " 7.2.3 ", major: 7, minor: 2, patch: 3},
		{in: "nonsense", wantErr: true},
		{in: "7", wantErr: true},
	} {
		got, err := ParseVersion(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("ParseVersion(%q) succeeded, want an error", tc.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseVersion(%q): %v", tc.in, err)
			continue
		}
		if got.Major != tc.major || got.Minor != tc.minor || got.Patch != tc.patch {
			t.Errorf("ParseVersion(%q) = %d.%d.%d", tc.in, got.Major, got.Minor, got.Patch)
		}
	}
}

func TestAtLeast(t *testing.T) {
	v, _ := ParseVersion("7.4.10")
	for _, tc := range []struct {
		major, minor int
		want         bool
	}{
		{6, 4, true}, {7, 0, true}, {7, 4, true}, {7, 6, false}, {8, 0, false},
	} {
		if got := v.AtLeast(tc.major, tc.minor); got != tc.want {
			t.Errorf("7.4.10.AtLeast(%d,%d) = %v", tc.major, tc.minor, got)
		}
	}
}

func TestCapabilities(t *testing.T) {
	v74, _ := ParseVersion("7.4.10")
	v60, _ := ParseVersion("6.0.30")
	if !v74.Supports(CapActiveAvailable) {
		t.Error("7.4 must support active_available")
	}
	if v60.Supports(CapActiveAvailable) {
		t.Error("6.0 must not claim active_available")
	}
	if !v60.Supports(CapHostGroupSelect) {
		t.Error("6.0 must support selectHostGroups")
	}
	if got := Requirement(CapBearerAuth); got != "6.4 or newer" {
		t.Errorf("Requirement = %q", got)
	}
}
