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

func TestValidationResult_String_NoIssues(t *testing.T) {
	r := &ValidationResult{}
	got := r.String()
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestValidationResult_String_ErrorsOnly(t *testing.T) {
	r := &ValidationResult{}
	r.AddError("bad field")
	r.AddError("missing value")
	got := r.String()
	want := "error: bad field; error: missing value"
	if got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

func TestValidationResult_String_WarningsOnly(t *testing.T) {
	r := &ValidationResult{}
	r.AddWarning("deprecated field")
	got := r.String()
	want := "warning: deprecated field"
	if got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

func TestValidationResult_String_Both(t *testing.T) {
	r := &ValidationResult{}
	r.AddError("err1")
	r.AddWarning("warn1")
	got := r.String()
	want := "error: err1; warning: warn1"
	if got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

func TestValidationResult_MergePreservesExisting(t *testing.T) {
	r := &ValidationResult{}
	r.AddError("e1")

	other := &ValidationResult{}
	other.AddWarning("w-from-other")

	r.Merge(other)

	if len(r.Errors) != 1 || r.Errors[0] != "e1" {
		t.Errorf("expected original error preserved, got %v", r.Errors)
	}
	if len(r.Warnings) != 1 || r.Warnings[0] != "w-from-other" {
		t.Errorf("expected merged warning, got %v", r.Warnings)
	}
}
