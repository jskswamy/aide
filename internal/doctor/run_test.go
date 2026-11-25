package doctor

import (
	"errors"
	"slices"
	"testing"
)

func fixable(id, subject string, argv ...string) Finding {
	return Finding{ID: id, Subject: subject, Severity: SeverityTip, Message: id + " " + subject, Fix: Fix{Argv: argv}}
}

func TestApplyFixes_AsksRunsRechecksAndStops(t *testing.T) {
	a := fixable("x.a", "1", "aide", "a")
	b := fixable("x.b", "2", "aide", "b") // gone after a's fix
	c := fixable("x.c", "3", "aide", "c") // declined
	d := fixable("x.d", "4", "aide", "d") // fails
	e := fixable("x.e", "5", "aide", "e") // never reached
	steps := Finding{ID: "x.steps", Subject: "s", Fix: Fix{Steps: []string{"do it"}}}
	state := []Finding{a, b, c, d, e, steps}
	recheck := func() Report { return NewReport("dev", nil, state) }
	var ran [][]string
	run := func(argv []string) (string, int, error) {
		ran = append(ran, argv)
		switch argv[1] {
		case "a":
			state = slices.DeleteFunc(state, func(f Finding) bool { return f.ID == "x.a" || f.ID == "x.b" })
			state = append(state, fixable("x.new", "9", "aide", "new"))
			return "ok", 0, nil
		case "d":
			return "boom", 1, nil
		}
		return "", 0, nil
	}
	ask := func(f Finding) bool { return f.ID != "x.c" }

	res := ApplyFixes(recheck(), true, ask, run, recheck)
	got := map[string]FixResult{}
	for _, r := range res {
		got[r.ID] = r
	}
	want := map[string]string{"x.a": "ran", "x.b": "skipped", "x.c": "not-run", "x.d": "failed", "x.e": "not-run", "x.new": "not-run"}
	for id, status := range want {
		if got[id].Status != status {
			t.Errorf("%s: status %q, want %q (%+v)", id, got[id].Status, status, got[id])
		}
	}
	if _, ok := got["x.steps"]; ok {
		t.Error("a finding with only steps got a fix result")
	}
	if len(ran) != 2 || got["x.d"].ExitCode != 1 || got["x.d"].Output != "boom" {
		t.Errorf("ran = %v, d = %+v", ran, got["x.d"])
	}
}

func TestApplyFixes_NoTerminalRunsNothing(t *testing.T) {
	r := NewReport("dev", nil, []Finding{fixable("x.a", "1", "aide", "a")})
	run := func([]string) (string, int, error) { t.Fatal("ran a fix without a terminal"); return "", 0, nil }
	res := ApplyFixes(r, false, nil, run, func() Report { return r })
	if len(res) != 1 || res[0].Status != "not-run" || res[0].Reason != "no terminal" {
		t.Errorf("results = %+v", res)
	}
}

func TestApplyFixes_RunErrorIsAFailure(t *testing.T) {
	r := NewReport("dev", nil, []Finding{fixable("x.a", "1", "aide", "a"), fixable("x.b", "2", "aide", "b")})
	run := func([]string) (string, int, error) { return "", -1, errors.New("exec failed") }
	res := ApplyFixes(r, true, func(Finding) bool { return true }, run, func() Report { return r })
	if res[0].Status != "failed" || res[0].Reason != "exec failed" || res[1].Status != "not-run" {
		t.Errorf("results = %+v", res)
	}
}
