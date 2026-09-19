package model

import (
	"testing"
	"time"
)

func TestFormAgeAt(t *testing.T) {
	birthDate := time.Date(2000, time.September, 19, 0, 0, 0, 0, time.UTC)
	form := &Form{BirthDate: &birthDate}

	testCases := []struct {
		name string
		date time.Time
		want int
	}{
		{
			name: "day before birthday",
			date: time.Date(2026, time.September, 18, 23, 59, 59, 0, time.UTC),
			want: 25,
		},
		{
			name: "birthday",
			date: time.Date(2026, time.September, 19, 0, 0, 0, 0, time.UTC),
			want: 26,
		},
		{
			name: "after birthday",
			date: time.Date(2026, time.September, 20, 0, 0, 0, 0, time.UTC),
			want: 26,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := form.AgeAt(testCase.date); got != testCase.want {
				t.Errorf("AgeAt() = %d, want %d", got, testCase.want)
			}
		})
	}
}

func TestFormAgeAtWithoutBirthDate(t *testing.T) {
	if got := (&Form{}).AgeAt(time.Now()); got != 0 {
		t.Errorf("AgeAt() = %d, want 0", got)
	}
}

func TestAgeSuffix(t *testing.T) {
	testCases := map[int]string{
		1:  "год",
		2:  "года",
		4:  "года",
		5:  "лет",
		11: "лет",
		14: "лет",
		21: "год",
		22: "года",
		24: "года",
	}

	for age, want := range testCases {
		if got := ageSuffix(age); got != want {
			t.Errorf("ageSuffix(%d) = %q, want %q", age, got, want)
		}
	}
}
