package log15

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"log/syslog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func testHandler() (Handler, *Record) {
	rec := new(Record)
	return FuncHandler(func(r Record) error {
		*rec = r
		return nil
	}), rec
}

func testLogger() (Logger, Handler, *Record) {
	l := New()
	h, r := testHandler()
	l.SetHandler(LazyHandler(h))
	return l, h, r
}

type testtype struct {
	name string
}

func (tt testtype) String() string {
	return tt.name
}

func TestLazy(t *testing.T) {
	t.Parallel()

	x := 1
	lazy := func() int { return x }

	l, _, r := testLogger()
	l.Info("", "x", Lazy{lazy})
	if r.Ctx[1] != 1 {
		t.Fatalf("Lazy function not evaluated, got %v, expected %d", r.Ctx[1], 1)
	}

	x = 2
	l.Info("", "x", Lazy{lazy})
	if r.Ctx[1] != 2 {
		t.Fatalf("Lazy function not evaluated, got %v, expected %d", r.Ctx[1], 2)
	}
}

func TestInvalidLazy(t *testing.T) {
	t.Parallel()

	l, _, r := testLogger()
	validate := func() {
		if len(r.Ctx) < 4 {
			t.Fatalf("Invalid lazy, got %d args, expecting at least 4", len(r.Ctx))
		}
		if r.Ctx[2] != errorKey {
			t.Fatalf("Invalid lazy, got key %s expecting %s", r.Ctx[2], errorKey)
		}
	}

	l.Info("", "x", Lazy{1})
	validate()

	l.Info("", "x", Lazy{func(x int) int { return x }})
	validate()

	l.Info("", "x", Lazy{func() {}})
	validate()
}

func TestCtx(t *testing.T) {
	t.Parallel()

	l, _, r := testLogger()
	l.Info("", Ctx{"x": 1, "y": "foo", "tester": t})
	if len(r.Ctx) != 6 {
		t.Fatalf("Expecting Ctx transformed into %d ctx args, got %d: %v", 6, len(r.Ctx), r.Ctx)
	}
}

func testFormatter(f Format) (Logger, *bytes.Buffer) {
	l := New()
	var buf bytes.Buffer
	l.SetHandler(StreamHandler(&buf, f))
	return l, &buf
}

func TestJson(t *testing.T) {
	t.Parallel()

	l, buf := testFormatter(JsonFormat())
	l.Error("some message", "x", 1, "y", 3.2)

	var v map[string]interface{}
	decoder := json.NewDecoder(buf)
	if err := decoder.Decode(&v); err != nil {
		t.Fatalf("Error decoding JSON: %v", err)
	}

	validate := func(key string, expected interface{}) {
		if v[key] != expected {
			t.Fatalf("Got %v expected %v for %v", v[key], expected, key)
		}
	}

	validate("msg", "some message")
	validate("x", float64(1))
	validate("y", 3.2)
	validate("lvl", "eror")
}

func TestJSONMap(t *testing.T) {
	t.Parallel()

	m := map[string]interface{}{
		"name":     "gopher",
		"age":      float64(5),
		"language": "go",
	}

	l, buf := testFormatter(JsonFormat())
	l.Error("logging structs", "struct", m)

	var v map[string]interface{}
	decoder := json.NewDecoder(buf)
	if err := decoder.Decode(&v); err != nil {
		t.Fatalf("Error decoding JSON: %v", err)
	}

	mv := v["struct"].(map[string]interface{})
	for key, expected := range m {
		if mv[key] != expected {
			t.Fatalf("Got %v expected %v for %v", mv[key], expected, key)
		}
	}
}

func TestLogfmt(t *testing.T) {
	t.Parallel()

	var nilVal *testtype

	l, buf := testFormatter(LogfmtFormat())
	l.Error("some message", "x", 1, "y", 3.2, "equals", "=", "quote", "\"",
		"nil", nilVal, "carriage_return", "bang"+string('\r')+"foo", "tab", "bar\tbaz", "newline", "foo\nbar")

	got := buf.Bytes()[27:buf.Len()]
	expected := []byte(`lvl=eror msg="some message" x=1 y=3.200 equals="=" quote="\"" nil=nil carriage_return="bang\rfoo" tab="bar\tbaz" newline="foo\nbar"` + "\n")
	if !bytes.Equal(got, expected) {
		t.Fatalf("Got %s, expected %s", got, expected)
	}
}

func TestFormatVariants(t *testing.T) {
	t.Parallel()

	record := Record{
		Time:     time.Unix(0, 0),
		Lvl:      LvlError,
		Msg:      "boom",
		Ctx:      []interface{}{"path", "/tmp/file", "count", 3, "ptr", (*testtype)(nil)},
		KeyNames: DefaultRecordKeyNames,
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

func TestMultiHandler(t *testing.T) {
	t.Parallel()

	h1, r1 := testHandler()
	h2, r2 := testHandler()
	l := New()
	l.SetHandler(MultiHandler(h1, h2))
	l.Debug("clone")

	if r1.Msg != "clone" || r2.Msg != "clone" {
		t.Fatalf("expected both handlers to receive the record, got %q and %q", r1.Msg, r2.Msg)
	}
}

type waitHandler struct {
	ch chan Record
}

func (h *waitHandler) Log(r Record) error {
	h.ch <- r
	return nil
}

func TestBufferedHandler(t *testing.T) {
	t.Parallel()

	ch := make(chan Record)
	l := New()
	l.SetHandler(BufferedHandler(0, &waitHandler{ch}))

	l.Debug("buffer")
	if r := <-ch; r.Msg != "buffer" {
		t.Fatalf("wrong value for r.Msg. Got %s expected %s", r.Msg, "buffer")
	}
}

func TestLogContext(t *testing.T) {
	t.Parallel()

	l, _, r := testLogger()
	l = l.New("foo", "bar")
	l.Crit("baz")

	if len(r.Ctx) != 2 || r.Ctx[0] != "foo" || r.Ctx[1] != "bar" {
		t.Fatalf("unexpected logger context: %#v", r.Ctx)
	}
}

func TestMapCtx(t *testing.T) {
	t.Parallel()

	l, _, r := testLogger()
	l.Crit("test", Ctx{"foo": "bar"})

	if len(r.Ctx) != 2 || r.Ctx[0] != "foo" || r.Ctx[1] != "bar" {
		t.Fatalf("unexpected context: %#v", r.Ctx)
	}
}

func TestLvlFilterHandler(t *testing.T) {
	t.Parallel()

	l := New()
	h, r := testHandler()
	l.SetHandler(LvlFilterHandler(LvlWarn, h))
	l.Info("info'd")

	if r.Msg != "" {
		t.Fatalf("Expected zero record, but got record with msg: %v", r.Msg)
	}

	l.Warn("warned")
	if r.Msg != "warned" {
		t.Fatalf("Got record msg %s expected %s", r.Msg, "warned")
	}
}

func TestNetHandler(t *testing.T) {
	t.Parallel()

	l, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Failed to listen: %v", err)
	}
	defer l.Close()

	errs := make(chan error, 1)
	go func() {
		c, err := l.Accept()
		if err != nil {
			errs <- err
			return
		}
		defer c.Close()

		rd := bufio.NewReader(c)
		s, err := rd.ReadString('\n')
		if err != nil {
			errs <- err
			return
		}

		got := s[27:]
		expected := "lvl=info msg=test x=1\n"
		if got != expected {
			errs <- errors.New("unexpected log line: " + got)
			return
		}
		errs <- nil
	}()

	lg := New()
	h, err := NetHandler("tcp", l.Addr().String(), LogfmtFormat())
	if err != nil {
		t.Fatal(err)
	}
	lg.SetHandler(h)
	lg.Info("test", "x", 1)

	select {
	case <-time.After(time.Second):
		t.Fatalf("test timed out")
	case err := <-errs:
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestMatchFilterHandler(t *testing.T) {
	t.Parallel()

	l, h, r := testLogger()
	l.SetHandler(MatchFilterHandler("err", nil, h))

	l.Crit("test", "foo", "bar")
	if r.Msg != "" {
		t.Fatalf("expected filter handler to discard msg")
	}

	l.Crit("test3", "err", nil)
	if r.Msg != "test3" {
		t.Fatalf("expected filter handler to allow msg")
	}
}

func TestMatchFilterBuiltin(t *testing.T) {
	t.Parallel()

	l, h, r := testLogger()
	l.SetHandler(MatchFilterHandler("lvl", LvlError, h))
	l.Info("does not pass")
	if r.Msg != "" {
		t.Fatalf("got info level record that should not have matched")
	}

	l.Error("error!")
	if r.Msg != "error!" {
		t.Fatalf("did not get error level record that should have matched")
	}

	r.Msg = ""
	l.SetHandler(MatchFilterHandler("msg", "matching message", h))
	l.Debug("matching message")
	if r.Msg != "matching message" {
		t.Fatalf("did not get record which matches")
	}
}

type failingWriter struct {
	fail bool
}

func (w *failingWriter) Write(buf []byte) (int, error) {
	if w.fail {
		return 0, errors.New("fail")
	}
	return len(buf), nil
}

func TestFailoverHandler(t *testing.T) {
	t.Parallel()

	l := New()
	h, r := testHandler()
	w := &failingWriter{}

	l.SetHandler(FailoverHandler(StreamHandler(w, JsonFormat()), h))
	l.Debug("test ok")
	if r.Msg != "" {
		t.Fatalf("expected no failover")
	}

	w.fail = true
	l.Debug("test failover", "x", 1)
	if r.Msg != "test failover" {
		t.Fatalf("expected failover")
	}
	if len(r.Ctx) != 4 || r.Ctx[2] != "failover_err_0" {
		t.Fatalf("unexpected failover context: %#v", r.Ctx)
	}
}

func TestIndependentSetHandler(t *testing.T) {
	t.Parallel()

	parent, _, r := testLogger()
	child := parent.New()
	child.SetHandler(DiscardHandler())
	parent.Info("test")
	if r.Msg != "test" {
		t.Fatalf("parent handler affected by child")
	}
}

func TestInheritHandler(t *testing.T) {
	t.Parallel()

	parent, _, r := testLogger()
	child := parent.New()
	parent.SetHandler(DiscardHandler())
	child.Info("test")
	if r.Msg == "test" {
		t.Fatalf("child handler should not use the original parent handler after swap")
	}
}

func TestConcurrent(t *testing.T) {
	root := New()
	const ctxLen = 34
	l := root.New(make([]interface{}, ctxLen)...)
	const goroutines = 8
	var res [goroutines]int

	l.SetHandler(SyncHandler(FuncHandler(func(r Record) error {
		res[r.Ctx[ctxLen+1].(int)]++
		return nil
	})))

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			for j := 0; j < 10000; j++ {
				l.Info("test message", "goroutine_idx", idx)
			}
		}(i)
	}
	wg.Wait()

	for _, val := range res[:] {
		if val != 10000 {
			t.Fatalf("Wrong number of messages for context: %+v", res)
		}
	}
}

type recordCollector struct {
	recs []Record
}

func (h *recordCollector) Log(r Record) error {
	h.recs = append(h.recs, r)
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
		if collector.recs[i].Msg != want.msg || collector.recs[i].Lvl != want.lvl {
			t.Fatalf("record %d = %#v, want msg=%q lvl=%v", i, collector.recs[i], want.msg, want.lvl)
		}
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
	if r.Ctx[0] != "key-only" || r.Ctx[1] != nil || r.Ctx[2] != errorKey {
		t.Fatalf("odd context was not normalized as expected: %#v", r.Ctx)
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
		_ = h.Log(Record{
			Time:     time.Unix(0, 0),
			Lvl:      LvlInfo,
			Msg:      "syslog smoke test",
			KeyNames: DefaultRecordKeyNames,
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
