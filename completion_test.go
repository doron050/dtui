package main

import (
	"bytes"
	"testing"
)

func TestCompletionCommandGeneratesScripts(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish"} {
		t.Run(shell, func(t *testing.T) {
			var out bytes.Buffer
			rootCmd.SetOut(&out)
			rootCmd.SetArgs([]string{"completion", shell})
			t.Cleanup(func() { rootCmd.SetOut(nil); rootCmd.SetArgs(nil) })

			if err := rootCmd.Execute(); err != nil {
				t.Fatalf("completion %s: %v", shell, err)
			}
			if out.Len() == 0 {
				t.Fatalf("completion %s produced no output", shell)
			}
		})
	}
}

func TestCompletionCommandRejectsUnknownShell(t *testing.T) {
	rootCmd.SetArgs([]string{"completion", "powershell"})
	t.Cleanup(func() { rootCmd.SetArgs(nil) })

	if err := rootCmd.Execute(); err == nil {
		t.Fatal("expected an error for an unsupported shell")
	}
}
