package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/piinstall/testfixture"
)

func TestSandboxPiManagedPreviewPreservesGuardEnv(t *testing.T) {
	for _, command := range []string{"show", "test"} {
		t.Run(command, func(t *testing.T) {
			home := isolatedConfigDir(t)
			f := testfixture.At(t, home, filepath.Join(home, ".pi", "agent"))
			gitConfig := filepath.Join(home, "guard-config", "gitconfig")
			testfixture.Write(t, gitConfig, "[user]\nname = Fixture\n", 0600)
			cfg := fmt.Sprintf("agents:\n  pi:\n    binary: %s\nprofiles:\n  work:\n    agent: pi\n    config_dir_name: work\n    env:\n      GIT_CONFIG_GLOBAL: %s\ndefault_profile: work\n", f.Launcher, gitConfig)
			testfixture.Write(t, filepath.Join(home, "xdg", "aide", "config.yaml"), cfg, 0600)
			out, err := runSandboxCmd(t, command)
			if err != nil || !strings.Contains(out, gitConfig) {
				t.Fatalf("guard override lost in preview: %v\n%s", err, out)
			}
		})
	}
}

func TestSandboxPiManagedPreview(t *testing.T) {
	for _, command := range []string{"show", "test"} {
		for _, invalid := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/invalid=%v", command, invalid), func(t *testing.T) {
				home := isolatedConfigDir(t)
				f := testfixture.At(t, home, filepath.Join(home, ".pi", "agent"))
				testfixture.Write(t, filepath.Join(home, "xdg", "aide", "config.yaml"), fmt.Sprintf("agents:\n  pi:\n    binary: %s\nprofiles:\n  work:\n    agent: pi\n    config_dir_name: work\ndefault_profile: work\n", f.Launcher), 0600)
				if invalid {
					testfixture.Write(t, filepath.Join(f.Root, "managed-install.json"), "{", 0600)
				}
				out, err := runSandboxCmd(t, command)
				if invalid {
					if err == nil {
						t.Fatalf("unsafe candidate preview accepted: %s", out)
					}
					return
				}
				if err != nil {
					t.Fatalf("preview: %v\n%s", err, out)
				}
				if !strings.Contains(out, f.Root) {
					t.Fatalf("preview omits installation: %s", out)
				}
			})
		}
	}
}
