package log15

import "testing"

var lvlCases = []struct {
	in  string
	out Lvl
}{
	{"warn", LvlWarn},
	{"eror", LvlError},
	{"error", LvlError},
	{"EROR", LvlError},
	{"ERROR", LvlError},
	{"UNK", -1},
}

func TestLvlFromString(t *testing.T) {
	t.Parallel()

	for _, tt := range lvlCases {
		lvl, err := LvlFromString(tt.in)
		if err != nil {
			if tt.out != -1 {
				t.Errorf("expected level %q but got err %v", tt.out, err)
			}
		} else {
			if lvl != tt.out {
				t.Errorf("LvlFromString(%q): want %q got %q", tt.in, tt.out, lvl)
			}
		}
	}
}

func TestLvlString(t *testing.T) {
	t.Parallel()

	cases := []struct {
		lvl  Lvl
		want string
	}{
		{LvlCrit, "crit"},
		{LvlError, "eror"},
		{LvlWarn, "warn"},
		{LvlInfo, "info"},
		{LvlDebug, "dbug"},
	}

	for _, tc := range cases {
		if got := tc.lvl.String(); got != tc.want {
			t.Fatalf("Lvl(%d).String() = %q, want %q", tc.lvl, got, tc.want)
		}
	}

	defer func() {
		if recover() == nil {
			t.Fatal("expected panic for invalid level")
		}
	}()
	_ = Lvl(99).String()
}
