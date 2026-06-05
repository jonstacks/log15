package term

import (
	"os"
	"testing"

	xterm "golang.org/x/term"
)

func TestIsTty(t *testing.T) {
	t.Parallel()

	if got, want := IsTty(os.Stdout.Fd()), xterm.IsTerminal(int(os.Stdout.Fd())); got != want {
		t.Fatalf("IsTty(stdout) = %v, want %v", got, want)
	}

	f, err := os.CreateTemp(t.TempDir(), "tty-check")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer f.Close()

	if IsTty(f.Fd()) {
		t.Fatal("IsTty reported temp file as a terminal")
	}
}
