package views

import (
	"testing"
	"time"
)

func TestParseBirthDate(t *testing.T) {
	birthDate, err := parseBirthDate("19.09.2000")
	if err != nil {
		t.Fatalf("parseBirthDate() returned an error: %v", err)
	}

	want := time.Date(2000, time.September, 19, 0, 0, 0, 0, time.UTC)
	if !birthDate.Equal(want) {
		t.Errorf("parseBirthDate() = %v, want %v", birthDate, want)
	}
}

func TestParseBirthDateRejectsMonthDayYearFormat(t *testing.T) {
	if _, err := parseBirthDate("09.19.2000"); err == nil {
		t.Error("parseBirthDate() accepted month.day.year format")
	}
}
