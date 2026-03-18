package config_test

import (
	"os"
	"testing"
)

func TestFixturesExist(t *testing.T) {
	t.Run("age key exists", func(t *testing.T) {
		info, err := os.Stat("../../testdata/age-key.txt")
		if err != nil {
			t.Fatalf("age key file not found: %v", err)
		}
		if info.Size() == 0 {
			t.Fatal("age key file is empty")
		}
	})

	t.Run("encrypted secrets file exists", func(t *testing.T) {
		info, err := os.Stat("../../testdata/test-secrets.enc.yaml")
		if err != nil {
			t.Fatalf("encrypted secrets file not found: %v", err)
		}
		if info.Size() == 0 {
			t.Fatal("encrypted secrets file is empty")
		}
	})
}
