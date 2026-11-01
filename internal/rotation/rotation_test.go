package rotation

import "testing"

func TestSettingsValidation(t *testing.T) {
	s := Defaults()
	if err := s.Validate(); err != nil {
		t.Fatalf("defaults invalid: %v", err)
	}
	if s.IntervalHours != 0 {
		t.Fatalf("default intervalHours = %d, want 0 (unlimited)", s.IntervalHours)
	}
	bad := []Settings{
		{CheckSeconds: 1},
		{CheckSeconds: 30, IntervalHours: -1},
		{CheckSeconds: 30, MinFreeBytes: 10, EmergencyFreeBytes: 20},
		{CheckSeconds: 30, MaxExchanges: -1},
	}
	for i, s := range bad {
		if err := s.Validate(); err == nil {
			t.Fatalf("case %d unexpectedly valid: %+v", i, s)
		}
	}
}
