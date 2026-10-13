package build

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Recipe entries run through a real bash on a pty, as on the build server.
func TestTerminalEntries(t *testing.T) {
	dir := t.TempDir()
	uid := fmt.Sprintf("hamtest-%d", time.Now().UnixNano())
	term, err := NewTerminal(uid)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		term.CloseTerminal()
		matches, _ := filepath.Glob("/tmp/" + uid + ".ham.*")
		for _, m := range matches {
			os.Remove(m)
		}
	}()

	run := func(index int, cmd string) error {
		if err := term.ExecTerminal(index, cmd); err != nil {
			return err
		}
		return term.WaitTerminal(index)
	}

	// cd and exports carry over to later entries.
	if err := run(0, "cd "+dir+"\nexport HAM_TEST_VAR=ok"); err != nil {
		t.Fatal("entry 0:", err)
	}

	// A command reading stdin must not swallow the lines after it.
	if err := run(1, "read -r x || true\ntouch after-read\n[ \"$HAM_TEST_VAR\" = ok ]\n[ \"$(pwd)\" = \""+dir+"\" ]"); err != nil {
		t.Fatal("entry 1:", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "after-read")); err != nil {
		t.Fatal("line after read was not executed:", err)
	}

	// Lines over the 4 KiB tty limit and "!" in double quotes.
	long := strings.Repeat("a", 6000)
	if err := run(2, "echo "+long+" > long.txt\necho \"!x\" > bang.txt"); err != nil {
		t.Fatal("entry 2:", err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "long.txt")); strings.TrimSpace(string(b)) != long {
		t.Fatalf("long line mangled (%d bytes)", len(b))
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "bang.txt")); strings.TrimSpace(string(b)) != "!x" {
		t.Fatalf("history expansion: %q", b)
	}

	// git must not wait for credentials on the terminal.
	if err := run(3, "[ \"$GIT_TERMINAL_PROMPT\" = 0 ]"); err != nil {
		t.Fatal("entry 3:", err)
	}

	// A failing entry is reported as failed.
	if err := run(4, "touch before-false\nfalse\ntouch after-false"); err == nil {
		t.Fatal("entry 4: failure not reported")
	}
	if _, err := os.Stat(filepath.Join(dir, "after-false")); err == nil {
		t.Fatal("entry 4: lines after the failing command ran")
	}
}
