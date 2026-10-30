package main

import (
	"testing"
	"time"
)

func TestCommentTimestampUsesBeijingTime(t *testing.T) {
	createdAt := time.Date(2026, 10, 30, 2, 40, 0, 0, time.UTC)
	if got, want := commentTimestamp(createdAt), "10-30 10:40"; got != want {
		t.Fatalf("commentTimestamp() = %q, want %q", got, want)
	}
}

func TestStampUsesBeijingTime(t *testing.T) {
	now := time.Now().In(beijingLocation)
	updatedAt := time.Date(now.Year(), now.Month(), now.Day(), 2, 40, 0, 0, time.UTC)
	if got, want := stamp(updatedAt), "10:40"; got != want {
		t.Fatalf("stamp() = %q, want %q", got, want)
	}
}
