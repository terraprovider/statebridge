package cmd

import (
	"bytes"
	"errors"
	"testing"

	"github.com/spf13/cobra"
)

func TestIntendedExitCode_SilencesCobraOutput(t *testing.T) {
	var output bytes.Buffer

	root := &cobra.Command{Use: "statebridge"}
	plan := &cobra.Command{
		Use: "plan",
		RunE: func(cmd *cobra.Command, args []string) error {
			return intendedExitCode(cmd, 2)
		},
	}
	root.AddCommand(plan)
	root.SetArgs([]string{"plan"})
	root.SetOut(&output)
	root.SetErr(&output)

	_, err := root.ExecuteC()
	if err == nil {
		t.Fatal("expected exit code error")
	}

	var exitErr *ExitCodeError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected ExitCodeError, got %T: %v", err, err)
	}
	if exitErr.Code != 2 {
		t.Errorf("expected exit code 2, got %d", exitErr.Code)
	}
	if got := output.String(); got != "" {
		t.Errorf("expected no Cobra output for intended exit code, got %q", got)
	}
}
