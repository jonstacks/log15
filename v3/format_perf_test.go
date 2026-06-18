package log15

import (
	"bytes"
	"errors"
	"testing"
	"time"
)

// stringerType implements fmt.Stringer.
type stringerType struct{ s string }

func (s stringerType) String() string { return s.s }

// errType implements error.
type errType struct{ s string }

func (e errType) Error() string { return e.s }

// equivalenceValues exercises every branch of value formatting, including
// types that need escaping, the nil-pointer-Stringer recover path, and a
// custom (non-string) type that falls through to fmt.Sprintf.
func equivalenceValues() []interface{} {
	var nilStringer *stringerType
	return []interface{}{
		nil,
		true,
		false,
		float32(3.0),
		float64(3.14159),
		float32(-2.5),
		int(42),
		int8(-8),
		int16(1600),
		int32(-32000),
		int64(64000000000),
		uint(7),
		uint8(255),
		uint16(65535),
		uint32(4000000000),
		uint64(18000000000000000000),
		"plain",
		"",
		"needs quote",
		"has=equals",
		"has\"quote",
		"tab\tnewline\nreturn\r",
		`back\slash`,
		time.Unix(0, 0).UTC(),
		time.Date(2026, 6, 18, 12, 30, 45, 0, time.UTC),
		errors.New("boom"),
		errType{"typed error"},
		stringerType{"i am a stringer"},
		stringerType{"stringer=needs\tescape"},
		nilStringer,              // typed nil Stringer -> must recover to "nil"
		LvlInfo,                  // Lvl is a Stringer
		struct{ A, B int }{1, 2}, // falls through to fmt.Sprintf
		[]int{1, 2, 3},
	}
}

// TestWriteLogfmtValueMatchesFormat asserts that the optimized buffer-writing
// formatter produces byte-for-byte the same output as the original
// string-returning formatLogfmtValue for a wide range of inputs.
func TestWriteLogfmtValueMatchesFormat(t *testing.T) {
	t.Parallel()
	for i, v := range equivalenceValues() {
		want := formatLogfmtValue(v)

		var buf bytes.Buffer
		writeLogfmtValue(&buf, v)
		got := buf.String()

		if got != want {
			t.Errorf("value[%d] %#v: writeLogfmtValue = %q, formatLogfmtValue = %q", i, v, got, want)
		}
	}
}

// TestLogfmtNonStringKey verifies the historical behavior for a context whose
// key isn't a string: the error key is emitted with an empty value.
func TestLogfmtNonStringKey(t *testing.T) {
	t.Parallel()
	r := Record{
		Time:     time.Unix(0, 0).UTC(),
		Lvl:      LvlInfo,
		Msg:      "m",
		Ctx:      []interface{}{42, "value"},
		KeyNames: DefaultRecordKeyNames,
	}
	got := string(LogfmtFormat().Format(r))
	if !bytes.Contains([]byte(got), []byte(errorKey+"=")) {
		t.Fatalf("expected %s= in output, got %q", errorKey, got)
	}
}

// BenchmarkLogfmtWithStringCtx covers the string-escaping value path which is
// the most common context shape in real applications.
func BenchmarkLogfmtWithStringCtx(b *testing.B) {
	r := Record{
		Time:     time.Now(),
		Lvl:      LvlInfo,
		Msg:      "test message",
		Ctx:      []interface{}{"method", "GET", "path", "/api/v1/users", "status", "ok"},
		KeyNames: DefaultRecordKeyNames,
	}
	logfmtFmt := LogfmtFormat()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logfmtFmt.Format(r)
	}
}

// BenchmarkLogfmtWithTimeCtx exercises the time.Time value path.
func BenchmarkLogfmtWithTimeCtx(b *testing.B) {
	r := Record{
		Time:     time.Now(),
		Lvl:      LvlInfo,
		Msg:      "test message",
		Ctx:      []interface{}{"start", time.Unix(0, 0), "deadline", time.Unix(1, 0)},
		KeyNames: DefaultRecordKeyNames,
	}
	logfmtFmt := LogfmtFormat()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logfmtFmt.Format(r)
	}
}
