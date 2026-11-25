package doctor

// FixResult records what --fix did with one finding's command.
type FixResult struct {
	ID       string `json:"id"`
	Subject  string `json:"subject"`
	Status   string `json:"status"` // ran, failed, skipped or not-run
	ExitCode int    `json:"exit_code"`
	Output   string `json:"output"`
	Reason   string `json:"reason"`
}

// ApplyFixes offers each finding's command in report order. ask is
// called once per fix (only when interactive); run executes argv. After
// every fix the checks run again, so a fix that also resolved a later
// finding skips it. Only findings present at the start are attempted,
// each once, and the first failure stops the run.
func ApplyFixes(r Report, interactive bool, ask func(Finding) bool, run func(argv []string) (string, int, error), recheck func() Report) []FixResult {
	var out []FixResult
	seen := map[string]bool{}
	current := r
	stopped := ""
	for _, f := range r.Findings {
		if len(f.Fix.Argv) == 0 {
			continue
		}
		key := f.ID + "\x00" + f.Subject
		seen[key] = true
		res := FixResult{ID: f.ID, Subject: f.Subject, Status: "not-run"}
		switch {
		case stopped != "":
			res.Reason = stopped
		case !interactive:
			res.Reason = "no terminal"
		case !has(current, key):
			res.Status, res.Reason = "skipped", "resolved by an earlier fix"
		case !ask(f):
			res.Reason = "declined"
		default:
			o, code, err := run(f.Fix.Argv)
			res.Output, res.ExitCode, res.Status = o, code, "ran"
			if err != nil || code != 0 {
				res.Status = "failed"
				if err != nil {
					res.Reason = err.Error()
				}
				stopped = "stopped after an earlier fix failed"
			}
			current = recheck()
		}
		out = append(out, res)
	}
	for _, f := range current.Findings {
		if len(f.Fix.Argv) > 0 && !seen[f.ID+"\x00"+f.Subject] {
			out = append(out, FixResult{ID: f.ID, Subject: f.Subject, Status: "not-run", Reason: "appeared after an earlier fix"})
		}
	}
	return out
}

func has(r Report, key string) bool {
	for _, f := range r.Findings {
		if f.ID+"\x00"+f.Subject == key {
			return true
		}
	}
	return false
}
