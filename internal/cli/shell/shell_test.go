package shell

import (
	"bytes"
	"strings"
	"testing"
)

func TestShellZshOutput(t *testing.T) {
	t.Parallel()
	cmd := newShellZshCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()

	// Check that the output contains key shell integration components
	if !strings.Contains(output, "__stackit_wrap") {
		t.Error("expected output to contain __stackit_wrap function")
	}
	if !strings.Contains(output, "__STACKIT_CD__") {
		t.Error("expected output to contain __STACKIT_CD__ directive parsing")
	}
	if !strings.Contains(output, "stackit()") {
		t.Error("expected output to contain stackit function wrapper")
	}
	if !strings.Contains(output, "st()") {
		t.Error("expected output to contain st function wrapper")
	}
	if !strings.Contains(output, "command stackit") {
		t.Error("expected output to call command stackit")
	}
	if !strings.Contains(output, "STACKIT_DIRECTIVE_FILE") {
		t.Error("expected output to use STACKIT_DIRECTIVE_FILE for TTY preservation")
	}
}

func TestShellBashOutput(t *testing.T) {
	t.Parallel()
	cmd := newShellBashCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()

	// Check that the output contains key shell integration components
	if !strings.Contains(output, "__stackit_wrap") {
		t.Error("expected output to contain __stackit_wrap function")
	}
	if !strings.Contains(output, "__STACKIT_CD__") {
		t.Error("expected output to contain __STACKIT_CD__ directive parsing")
	}
	if !strings.Contains(output, "st()") {
		t.Error("expected output to contain st function wrapper")
	}
	if !strings.Contains(output, "STACKIT_DIRECTIVE_FILE") {
		t.Error("expected output to use STACKIT_DIRECTIVE_FILE for TTY preservation")
	}
}

func TestShellFishOutput(t *testing.T) {
	t.Parallel()
	cmd := newShellFishCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()

	// Check that the output contains key shell integration components
	if !strings.Contains(output, "function stackit") {
		t.Error("expected output to contain function stackit")
	}
	if !strings.Contains(output, "function st") {
		t.Error("expected output to contain function st")
	}
	if !strings.Contains(output, "__STACKIT_CD__") {
		t.Error("expected output to contain __STACKIT_CD__ directive parsing")
	}
	if !strings.Contains(output, "command stackit") {
		t.Error("expected output to call command stackit")
	}
	if !strings.Contains(output, "STACKIT_DIRECTIVE_FILE") {
		t.Error("expected output to use STACKIT_DIRECTIVE_FILE for TTY preservation")
	}
}
