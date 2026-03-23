package seatbelt

import "testing"

func TestValidationResult_AddError(t *testing.T) {
	r := &ValidationResult{}
	r.AddError("field %q is required", "HomeDir")
	if len(r.Errors) != 1 {
		t.Fatalf("expected 1 error, got %d", len(r.Errors))
	}
	if r.Errors[0] != `field "HomeDir" is required` {
		t.Errorf("unexpected error: %s", r.Errors[0])
	}
}

func TestValidationResult_OK(t *testing.T) {
	r := &ValidationResult{}
	if !r.OK() {
		t.Error("empty result should be OK")
	}
	r.AddError("bad")
	if r.OK() {
		t.Error("result with error should not be OK")
	}
}

func TestValidationResult_Err(t *testing.T) {
	r := &ValidationResult{}
	if r.Err() != nil {
		t.Error("empty result should return nil Err")
	}
	r.AddError("first")
	r.AddError("second")
	if r.Err() == nil {
		t.Error("expected non-nil Err")
	}
	if r.Err().Error() != "first" {
		t.Errorf("Err should return first error, got %q", r.Err().Error())
	}
}

func TestValidationResult_Merge(t *testing.T) {
	r1 := &ValidationResult{}
	r1.AddError("e1")
	r1.AddWarning("w1")
	r2 := &ValidationResult{}
	r2.AddError("e2")
	r2.AddWarning("w2")
	r1.Merge(r2)
	if len(r1.Errors) != 2 || len(r1.Warnings) != 2 {
		t.Errorf("expected 2 errors + 2 warnings, got %d + %d", len(r1.Errors), len(r1.Warnings))
	}
}

func TestValidationResult_MergeNil(t *testing.T) {
	r := &ValidationResult{}
	r.Merge(nil) // should not panic
	if !r.OK() {
		t.Error("merge nil should not add errors")
	}
}
