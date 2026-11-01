package version

import "testing"

func TestParseCLIOutput(t *testing.T) {
	cases := []struct {
		in      string
		base    string
		build   int
		commit  string
		release string
	}{
		{"1.39.51+f4a52d1e00\n", "1.39.51", 0, "f4a52d1e00", ""},
		{"fluxzy 1.39.52.38106", "1.39.52", 38106, "", ""},
		{"v1.39.51.62629", "1.39.51", 62629, "", ""},
		{"1.40.0", "1.40.0", 0, "", ""},
	}
	for _, tc := range cases {
		got, err := ParseCLIOutput(tc.in)
		if err != nil {
			t.Fatalf("%q: %v", tc.in, err)
		}
		if got.Base() != tc.base || got.Build != tc.build || got.Commit != tc.commit || got.ReleaseTag != tc.release {
			t.Fatalf("%q: %+v", tc.in, got)
		}
	}
}

func TestParseReleaseTag(t *testing.T) {
	got, err := ParseReleaseTag("v1.39.51.62629")
	if err != nil {
		t.Fatal(err)
	}
	if got.Base() != "1.39.51" || got.Build != 62629 || got.ReleaseTag != "1.39.51.62629" {
		t.Fatalf("%+v", got)
	}
}

func TestCompareBaseIgnoresBuild(t *testing.T) {
	a, _ := ParseCLIOutput("1.39.51+f4a52d1e00")
	b, _ := ParseReleaseTag("v1.39.51.62629")
	if CompareBase(a, b) != 0 {
		t.Fatalf("same base should compare equal")
	}
	c, _ := ParseReleaseTag("v1.39.52.1")
	if CompareBase(a, c) >= 0 {
		t.Fatalf("newer base must compare greater")
	}
}

func TestCompareRelease(t *testing.T) {
	c, err := CompareRelease("1.39.52.38106", "1.39.51.62629")
	if err != nil || c != 1 {
		t.Fatalf("compare=%d err=%v", c, err)
	}
	c, err = CompareRelease("v1.39.51", "v1.39.51.0")
	if err != nil || c != 0 {
		t.Fatalf("compare=%d err=%v", c, err)
	}
}
