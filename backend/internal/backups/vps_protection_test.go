package backups

import (
	"testing"
	"time"
)

func TestNextScheduledRunDaily(t *testing.T) {
	location := time.FixedZone("NPT", 5*60*60+45*60)
	now := time.Date(2026, time.July, 23, 3, 0, 0, 0, location)
	clock := time.Date(1, 1, 1, 2, 0, 0, 0, time.UTC)

	got := nextScheduledRun(now, "daily", nil, nil, clock, location).In(location)
	want := time.Date(2026, time.July, 24, 2, 0, 0, 0, location)
	if !got.Equal(want) {
		t.Fatalf("next daily run = %s, want %s", got, want)
	}
}

func TestNextScheduledRunWeekly(t *testing.T) {
	location := time.UTC
	now := time.Date(2026, time.July, 23, 10, 0, 0, 0, location) // Thursday
	clock := time.Date(1, 1, 1, 9, 30, 0, 0, location)
	monday := int(time.Monday)

	got := nextScheduledRun(now, "weekly", &monday, nil, clock, location)
	want := time.Date(2026, time.July, 27, 9, 30, 0, 0, location)
	if !got.Equal(want) {
		t.Fatalf("next weekly run = %s, want %s", got, want)
	}
}

func TestNextScheduledRunMonthlyMovesToNextMonth(t *testing.T) {
	location := time.UTC
	now := time.Date(2026, time.July, 23, 10, 0, 0, 0, location)
	clock := time.Date(1, 1, 1, 2, 15, 0, 0, location)
	day := 10

	got := nextScheduledRun(now, "monthly", nil, &day, clock, location)
	want := time.Date(2026, time.August, 10, 2, 15, 0, 0, location)
	if !got.Equal(want) {
		t.Fatalf("next monthly run = %s, want %s", got, want)
	}
}

func TestValidateSchedule(t *testing.T) {
	tests := []struct {
		name      string
		input     UpsertVPSBackupPolicyInput
		frequency string
		wantErr   bool
	}{
		{
			name:      "valid weekly",
			input:     UpsertVPSBackupPolicyInput{LocalTime: "02:00", Timezone: "UTC", Weekday: intPtr(2)},
			frequency: "weekly",
		},
		{
			name:      "weekly day required",
			input:     UpsertVPSBackupPolicyInput{LocalTime: "02:00", Timezone: "UTC"},
			frequency: "weekly",
			wantErr:   true,
		},
		{
			name:      "monthly day limited to 28",
			input:     UpsertVPSBackupPolicyInput{LocalTime: "02:00", Timezone: "UTC", MonthDay: intPtr(29)},
			frequency: "monthly",
			wantErr:   true,
		},
		{
			name:      "invalid timezone",
			input:     UpsertVPSBackupPolicyInput{LocalTime: "02:00", Timezone: "Not/AZone"},
			frequency: "daily",
			wantErr:   true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := validateSchedule(test.input, test.frequency)
			if (err != nil) != test.wantErr {
				t.Fatalf("validateSchedule() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}

func intPtr(value int) *int { return &value }
