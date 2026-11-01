package retention

import "testing"

func TestValidateSettings(t *testing.T) {
	valid := []Settings{
		{HotHours: 24, ColdDays: 30, SamplePercent: 5},
		{HotHours: 0, ColdDays: 0, SamplePercent: 0},
		{HotHours: 48, ColdDays: 2, SamplePercent: 100},
	}
	for _, s := range valid {
		if err := validateSettings(s); err != nil {
			t.Fatalf("expected valid %+v: %v", s, err)
		}
	}

	invalid := []Settings{
		{HotHours: -1},
		{HotHours: 72, ColdDays: 2, SamplePercent: 5},
		{HotHours: 1, ColdDays: 1, SamplePercent: 101},
		{HotHours: 8761},
	}
	for _, s := range invalid {
		if err := validateSettings(s); err == nil {
			t.Fatalf("expected invalid %+v", s)
		}
	}
}

func TestShouldSampleDeterministic(t *testing.T) {
	a := shouldSample("session", 42, 5)
	b := shouldSample("session", 42, 5)
	if a != b {
		t.Fatal("sampling must be deterministic")
	}
	if shouldSample("session", 42, 0) {
		t.Fatal("0 percent must sample nothing")
	}
	if !shouldSample("session", 42, 100) {
		t.Fatal("100 percent must sample everything")
	}
}
