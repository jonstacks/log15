package ext

import (
	"errors"
	"math/rand"
	"os"
	"os/exec"
	"testing"

	log "github.com/inconshreveable/log15"
)

func TestRandId(t *testing.T) {
	t.Parallel()

	if id := RandId(8); len(id) != 16 {
		t.Fatalf("RandId(8) length = %d, want 16", len(id))
	}
	if id := RandId(0); id != "" {
		t.Fatalf("RandId(0) = %q, want empty string", id)
	}
}

func TestLockedSource(t *testing.T) {
	t.Parallel()

	src := &lockedSource{src: rand.NewSource(1)}
	if got := src.Int63(); got != rand.NewSource(1).Int63() {
		t.Fatalf("Int63() = %d, want deterministic seeded value", got)
	}

	src.Seed(2)
	want := rand.NewSource(2).Int63()
	if got := src.Int63(); got != want {
		t.Fatalf("Int63() after Seed = %d, want %d", got, want)
	}
}

func TestFatalHandlerReturnsWrappedError(t *testing.T) {
	t.Parallel()

	want := errors.New("write failed")
	h := FatalHandler(log.FuncHandler(func(r *log.Record) error {
		return want
	}))

	if err := h.Log(&log.Record{Lvl: log.LvlError, Msg: "not fatal"}); !errors.Is(err, want) {
		t.Fatalf("FatalHandler returned %v, want %v", err, want)
	}
}

func TestFatalHandlerExitsOnCrit(t *testing.T) {
	if os.Getenv("LOG15_FATAL_HELPER") == "1" {
		h := FatalHandler(log.DiscardHandler())
		_ = h.Log(&log.Record{Lvl: log.LvlCrit, Msg: "fatal"})
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestFatalHandlerExitsOnCrit")
	cmd.Env = append(os.Environ(), "LOG15_FATAL_HELPER=1")

	err := cmd.Run()
	if err == nil {
		t.Fatal("expected subprocess to exit with status 1")
	}

	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("expected ExitError, got %T", err)
	}
	if exitErr.ExitCode() != 1 {
		t.Fatalf("exit code = %d, want 1", exitErr.ExitCode())
	}
}
