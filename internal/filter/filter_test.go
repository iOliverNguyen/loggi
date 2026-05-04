package filter

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/iOliverNguyen/loggi/internal/store"
)

func setup(t *testing.T) *store.Store {
	t.Helper()
	s := store.New(store.Options{Cap: 64})
	rows := []map[string]any{
		{"level": "info", "service": "batch_worker", "msg": "dispatcher started", "ts": 1.0},
		{"level": "warn", "service": "cron_worker", "msg": "kafka timeout retry", "ts": 2.0},
		{"level": "error", "service": "mapper", "msg": "panic recovered", "ts": 3.0},
		{"level": "debug", "service": "mapper", "msg": "trace_id=xyz123", "ts": 4.0,
			"msg_batch_conf": map[string]any{"ConsumerQos": 50}},
	}
	for _, r := range rows {
		raw, _ := json.Marshal(r)
		s.Publish(store.AppendInput{JSON: raw})
	}
	return s
}

func count(s *store.Store, expr string) (int, error) {
	n, err := Parse(expr)
	if err != nil {
		return 0, err
	}
	fn := Compile(n, s)
	c := 0
	for seq := s.Tail(); seq < s.Head(); seq++ {
		if fn == nil || fn(seq) {
			c++
		}
	}
	return c, nil
}

func TestSimpleEquality(t *testing.T) {
	s := setup(t)
	got, err := count(s, "level:error")
	if err != nil {
		t.Fatal(err)
	}
	if got != 1 {
		t.Fatalf("want 1 got %d", got)
	}
}

func TestImplicitAnd(t *testing.T) {
	s := setup(t)
	got, err := count(s, "level:debug service:mapper")
	if err != nil {
		t.Fatal(err)
	}
	if got != 1 {
		t.Fatalf("want 1 got %d", got)
	}
}

func TestOr(t *testing.T) {
	s := setup(t)
	got, err := count(s, "service:batch_worker OR service:cron_worker")
	if err != nil {
		t.Fatal(err)
	}
	if got != 2 {
		t.Fatalf("want 2 got %d", got)
	}
}

func TestSubstr(t *testing.T) {
	s := setup(t)
	got, err := count(s, "*timeout*")
	if err != nil {
		t.Fatal(err)
	}
	if got != 1 {
		t.Fatalf("want 1 got %d", got)
	}
}

func TestNegation(t *testing.T) {
	s := setup(t)
	got, err := count(s, "-level:debug")
	if err != nil {
		t.Fatal(err)
	}
	if got != 3 {
		t.Fatalf("want 3 got %d", got)
	}
}

func TestLevelOrdinal(t *testing.T) {
	s := setup(t)
	got, err := count(s, "level:>=warn")
	if err != nil {
		t.Fatal(err)
	}
	if got != 2 {
		t.Fatalf("want 2 got %d", got)
	}
}

func TestRange(t *testing.T) {
	s := setup(t)
	got, err := count(s, "ts:[2..3]")
	if err != nil {
		t.Fatal(err)
	}
	if got != 2 {
		t.Fatalf("want 2 got %d", got)
	}
}

func TestNestedPath(t *testing.T) {
	s := setup(t)
	got, err := count(s, "@msg_batch_conf.ConsumerQos:>=50")
	if err != nil {
		t.Fatal(err)
	}
	if got != 1 {
		t.Fatalf("want 1 got %d", got)
	}
}

func TestEmptyMatchesAll(t *testing.T) {
	s := setup(t)
	got, err := count(s, "")
	if err != nil {
		t.Fatal(err)
	}
	if got != 4 {
		t.Fatalf("want 4 got %d", got)
	}
}

func TestParseErrors(t *testing.T) {
	bad := []string{"(level:error", "level:[1..", "level:>="}
	for _, b := range bad {
		_, err := Parse(b)
		if err == nil {
			t.Errorf("want parse error for %q", b)
		}
	}
}

func TestParens(t *testing.T) {
	s := setup(t)
	got, err := count(s, "(level:warn OR level:error) AND -service:mapper")
	if err != nil {
		t.Fatal(err)
	}
	if got != 1 {
		t.Fatalf("want 1 got %d", got)
	}
	_ = fmt.Sprint("ok")
}
