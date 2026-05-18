package provision_test

import (
	"errors"
	"testing"

	"github.com/jskswamy/aide/internal/provision"
)

func TestJournalRollbackInReverseOrder(t *testing.T) {
	var order []string
	j := &provision.Journal{}
	j.Record(func() error { order = append(order, "undo-1"); return nil })
	j.Record(func() error { order = append(order, "undo-2"); return nil })
	j.Record(func() error { order = append(order, "undo-3"); return nil })
	if err := j.Rollback(); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	want := []string{"undo-3", "undo-2", "undo-1"}
	if len(order) != 3 || order[0] != want[0] || order[1] != want[1] || order[2] != want[2] {
		t.Errorf("order = %v, want %v", order, want)
	}
}

func TestJournalRollbackContinuesOnInverseFailure(t *testing.T) {
	var attempts []string
	j := &provision.Journal{}
	j.Record(func() error { attempts = append(attempts, "ok-1"); return nil })
	j.Record(func() error { attempts = append(attempts, "fail-2"); return errors.New("boom") })
	j.Record(func() error { attempts = append(attempts, "ok-3"); return nil })
	err := j.Rollback()
	if err == nil {
		t.Fatal("expected aggregate error from rollback")
	}
	if len(attempts) != 3 {
		t.Errorf("expected all 3 inverses attempted, got %v", attempts)
	}
}
