package domain

import (
	"testing"
	"time"
)

func TestAdoptedApplicationCannotBeAdoptedAgain(t *testing.T) {
	application := Application{ID: "application-adopted", Status: ApplicationAdopted, Version: 3}
	if _, err := application.Transition(ApplicationAdopted, time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)); err == nil {
		t.Fatalf("adopted application accepted a second adoption: %v", err)
	}
}
