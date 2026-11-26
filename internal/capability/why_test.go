package capability

import (
	"testing"
	"testing/fstest"
)

func TestDetectProject_IgnoresIndirectGoDependencies(t *testing.T) {
	fsys := fstest.MapFS{"go.mod": {Data: []byte("module x\nrequire (\n\tgithub.com/aws/aws-sdk-go-v2 v1.4.1 // indirect\n\tcloud.google.com/go v0.1.0 // indirect\n)\n")}}
	for _, name := range DetectProject(fsys) {
		if name == "aws" || name == "gcp" {
			t.Errorf("%s suggested from an indirect dependency", name)
		}
	}
	fsys["go.mod"] = &fstest.MapFile{Data: []byte("module x\nrequire github.com/aws/aws-sdk-go-v2 v1.4.1\n")}
	if got := WhyDetected(fsys, "aws"); got != `go.mod mentions "aws-sdk-go"` {
		t.Errorf("WhyDetected(aws) = %q", got)
	}
}

func TestWhyDetected(t *testing.T) {
	fsys := fstest.MapFS{
		"pyproject.toml":  {Data: []byte("")},
		"k8s/deploy.yaml": {Data: []byte("apiVersion: v1\n")},
	}
	for name, want := range map[string]string{
		"go":     "",
		"python": "pyproject.toml present",
		"k8s":    "k8s/ directory present",
	} {
		if got := WhyDetected(fsys, name); got != want {
			t.Errorf("WhyDetected(%s) = %q, want %q", name, got, want)
		}
	}
}
