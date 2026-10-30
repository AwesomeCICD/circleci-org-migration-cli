package cmd

import (
	"testing"
	"time"
)

func TestManPageDate_PinnedByDefault(t *testing.T) {
	t.Setenv("SOURCE_DATE_EPOCH", "")
	want := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)
	if got := manPageDate(); !got.Equal(want) {
		t.Fatalf("manPageDate() = %v, want %v", got, want)
	}
}

func TestManPageDate_SourceDateEpochOverride(t *testing.T) {
	t.Setenv("SOURCE_DATE_EPOCH", "1793300000")
	want := time.Unix(1793300000, 0).UTC()
	if got := manPageDate(); !got.Equal(want) {
		t.Fatalf("manPageDate() = %v, want %v", got, want)
	}
}
