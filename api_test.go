package log15

import (
	"bytes"
	"errors"
	"log/syslog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type recordCollector struct {
	recs []*Record
}

func (h *recordCollector) Log(r *Record) error {
	rec := *r
	h.recs = append(h.recs, &rec)
	return nil
}

func TestRootConvenienceFunctions(t *testing.T) {
	collector := &recordCollector{}
	previous := root.GetHandler()
	root.SetHandler(collector)
	defer root.SetHandler(previous)

	if Root() == nil {
		t.Fatal("Root returned nil")
	}

	Debug("debug message", "k", 1)
	Info("info message")
	Warn("warn message")
	Error("error message")
	Crit("crit message")

	if len(collector.recs) != 5 {
		t.Fatalf("got %d records, want 5", len(collector.recs))
	}

	wants := []struct {
		msg string
		lvl Lvl
	}{
		{"debug message", LvlDebug},
		{"info message", LvlInfo},
		{"warn message", LvlWarn},
		{"error message", LvlError},
		{"crit message", LvlCrit},
	}

	for i, want := range wants {
		if collector.recs[i].Msg != want.msg {
			t.Fatalf("record %d msg = %q, want %q", i, collector.recs[i].Msg, want.msg)
		}
		if collector.recs[i].Lvl != want.lvl {
			t.Fatalf("record %d level = %v, want %v", i, collector.recs[i].Lvl, want.lvl)
		}
	}

	if got := collector.recs[0].Ctx; len(got) != 2 || got[0] != "k" || got[1] != 1 {
		t.Fatalf("unexpected context from Debug: %#v", got)
	}
}

func TestGetHandlerAndOddContextNormalization(t *testing.T) {
	t.Parallel()

	l, _, r := testLogger()
	if l.GetHandler() == nil {
		t.Fatal("GetHandler returned nil")
	}

	l.Info("odd context", "key-only")
	if got := len(r.Ctx); got != 4 {
		t.Fatalf("got %d ctx entries, want 4", got)
	}
	if r.Ctx[0] != "key-only" || r.Ctx[1] != nil {
		t.Fatalf("odd context was not normalized as expected: %#v", r.Ctx)
	}
	if r.Ctx[2] != errorKey {
		t.Fatalf("expected %q key, got %#v", errorKey, r.Ctx[2])
	}
}

func TestFormatVariants(t *testing.T) {
	t.Parallel()

	record := &Record{
		Time: time.Unix(0, 0),
		Lvl:  LvlError,
		Msg:  "boom",
		Ctx:  []interface{}{"path", "/tmp/file", "count", 3, "ptr", (*testtype)(nil)},
		KeyNames: RecordKeyNames{
			Time: timeKey,
			Msg:  msgKey,
			Lvl:  lvlKey,
		},
	}

	terminal := string(TerminalFormat().Format(record))
	if !strings.Contains(terminal, "\x1b[31mEROR\x1b[0m") {
		t.Fatalf("terminal format did not colorize error level: %q", terminal)
	}
	if !strings.Contains(terminal, "/tmp/file") || !strings.Contains(terminal, "count") {
		t.Fatalf("terminal format missing context: %q", terminal)
	}

	pretty := string(JsonFormatEx(true, false).Format(record))
	if strings.HasSuffix(pretty, "\n") {
		t.Fatalf("pretty JSON unexpectedly ended with a newline: %q", pretty)
	}
	if !strings.Contains(pretty, "\n    ") {
		t.Fatalf("pretty JSON was not indented: %q", pretty)
	}
	if !strings.Contains(pretty, `"ptr": "nil"`) {
		t.Fatalf("pretty JSON did not stringify nil pointer Stringer: %q", pretty)
	}
}

func TestFileHandlerAndMust(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log.txt")

	h, err := FileHandler(path, LogfmtFormat())
	if err != nil {
		t.Fatalf("FileHandler returned error: %v", err)
	}

	l := New()
	l.SetHandler(h)
	l.Info("written to file", "x", 7)

	closer, ok := h.(closingHandler)
	if !ok {
		t.Fatalf("FileHandler returned %T, want closingHandler", h)
	}
	if err := (&closer).Close(); err != nil {
		t.Fatalf("closing handler returned error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}
	if !bytes.Contains(data, []byte(`msg="written to file"`)) {
		t.Fatalf("log file missing message: %q", data)
	}

	if Must.FileHandler(path, LogfmtFormat()) == nil {
		t.Fatal("Must.FileHandler returned nil")
	}

	if _, err := FileHandler(t.TempDir(), LogfmtFormat()); err == nil {
		t.Fatal("expected FileHandler to fail when path is a directory")
	}

	defer func() {
		if recover() == nil {
			t.Fatal("expected Must.FileHandler to panic for invalid path")
		}
	}()
	_ = Must.FileHandler(t.TempDir(), LogfmtFormat())
}

func TestMustNetHandlerPanics(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Fatal("expected Must.NetHandler to panic")
		}
	}()
	_ = Must.NetHandler("invalid-network", "addr", LogfmtFormat())
}

func TestSyslogAPIs(t *testing.T) {
	t.Parallel()

	h, err := SyslogHandler(syslog.LOG_INFO, "log15-test", LogfmtFormat())
	if err == nil {
		if h == nil {
			t.Fatal("SyslogHandler returned nil handler without an error")
		}
		_ = h.Log(&Record{
			Time: time.Unix(0, 0),
			Lvl:  LvlInfo,
			Msg:  "syslog smoke test",
			KeyNames: RecordKeyNames{
				Time: timeKey,
				Msg:  msgKey,
				Lvl:  lvlKey,
			},
		})
	}

	if _, err := SyslogNetHandler("invalid-network", "addr", syslog.LOG_INFO, "log15-test", LogfmtFormat()); err == nil {
		t.Fatal("expected SyslogNetHandler to fail for invalid network")
	}

	panicked := false
	var mustHandler Handler
	func() {
		defer func() {
			if recover() != nil {
				panicked = true
			}
		}()
		mustHandler = Must.SyslogHandler(syslog.LOG_INFO, "log15-test", LogfmtFormat())
	}()
	if !panicked && mustHandler == nil {
		t.Fatal("Must.SyslogHandler returned nil without panicking")
	}

	defer func() {
		if recover() == nil {
			t.Fatal("expected Must.SyslogNetHandler to panic")
		}
	}()
	_ = Must.SyslogNetHandler("invalid-network", "addr", syslog.LOG_INFO, "log15-test", LogfmtFormat())
}

func TestSharedSyslogReturnsProvidedError(t *testing.T) {
	t.Parallel()

	want := errors.New("syslog unavailable")
	if _, err := sharedSyslog(LogfmtFormat(), nil, want); !errors.Is(err, want) {
		t.Fatalf("sharedSyslog error = %v, want %v", err, want)
	}
}
