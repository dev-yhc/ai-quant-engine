package config

import "testing"

func TestDefaultScheduleRunsDailyAt23KST(t *testing.T) {
	for _, key := range []string{"TEMPORAL_SCHEDULE_CRON", "TEMPORAL_SCHEDULE_TIME_ZONE"} {
		t.Setenv(key, "")
	}
	settings := fromEnvironment()
	if settings.TemporalScheduleCron != "0 23 * * *" {
		t.Fatalf("cron = %q", settings.TemporalScheduleCron)
	}
	if settings.TemporalScheduleTimeZone != "Asia/Seoul" {
		t.Fatalf("timezone = %q", settings.TemporalScheduleTimeZone)
	}
}
