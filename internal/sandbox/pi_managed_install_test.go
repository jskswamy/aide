package sandbox

import (
	"github.com/jskswamy/aide/internal/piinstall"
	"github.com/jskswamy/aide/internal/piinstall/testfixture"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestValidatePiManagedInstall(t *testing.T) {
	for _, tc := range []struct {
		name, backend, denial string
		present, fail         bool
	}{
		{"clear-landlock", BackendLandlock, "", false, false},
		{"root", BackendSeatbelt, "root", true, true},
		{"filesystem-root", BackendLandlock, "filesystem-root", true, true},
		{"ancestor", BackendBwrap, "ancestor", true, true},
		{"landlock-child", BackendLandlock, "child", true, true},
		{"landlock-future", BackendLandlock, "child", false, true},
		{"bwrap-future", BackendBwrap, "child", false, true},
		{"bwrap-child", BackendBwrap, "child", true, false},
		{"seatbelt-child", BackendSeatbelt, "child", true, false},
		{"unmatched-glob", BackendSeatbelt, "glob", false, true},
		{"launcher", BackendLandlock, "launcher", true, true},
		{"none", BackendNone, "", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := testfixture.New(t)
			p := DefaultPolicy(Paths{HomeDir: f.Home, ProjectRoot: t.TempDir(), RuntimeDir: t.TempDir(), TempDir: t.TempDir()}, nil)
			in := piinstall.Installation{Root: f.Root, Readable: []string{f.Launcher}}
			denial := ""
			switch tc.denial {
			case "filesystem-root":
				denial = "/"
			case "root":
				denial = f.Root
			case "ancestor":
				denial = f.Agent
			case "child":
				denial = filepath.Join(f.Root, "releases", "private.txt")
			case "glob":
				denial = filepath.Join(f.Root, "future-*", "secret")
			case "launcher":
				denial = f.Launcher
			}
			if tc.present && tc.denial == "child" {
				testfixture.Write(t, denial, "secret", 0600)
			}
			if denial != "" {
				p.ExtraDenied = []string{denial}
			}
			before := p
			err := ValidatePiManagedInstall(p, in, tc.backend)
			if (err != nil) != tc.fail {
				t.Fatalf("error=%v fail=%v", err, tc.fail)
			}
			if err != nil && (!strings.Contains(err.Error(), tc.backend) || denial != "" && !strings.Contains(err.Error(), denial)) {
				t.Errorf("not actionable: %v", err)
			}
			if !reflect.DeepEqual(p, before) {
				t.Fatal("validation mutated policy")
			}
		})
	}
}

func TestValidatePiManagedInstallResolvedDeny(t *testing.T) {
	f := testfixture.New(t)
	alias := filepath.Join(f.Home, "deny-link")
	testfixture.Link(t, f.Root, alias)
	p := Policy{ExtraDenied: []string{alias}}
	if err := ValidatePiManagedInstall(p, piinstall.Installation{Root: f.Root}, BackendLandlock); err == nil {
		t.Fatal("symlink denial lost")
	}
}
