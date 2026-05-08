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

// `field:*` (and any all-stars variant) is the existence predicate:
// the field must be set & non-empty.
func TestExistsPredicate(t *testing.T) {
	s := setup(t)
	got, err := count(s, "service:*")
	if err != nil {
		t.Fatal(err)
	}
	if got != 4 {
		t.Fatalf("service:* want 4 got %d", got)
	}
	// Nested path: only one row has @msg_batch_conf set.
	got, err = count(s, "@msg_batch_conf.ConsumerQos:*")
	if err != nil {
		t.Fatal(err)
	}
	if got != 1 {
		t.Fatalf("@msg_batch_conf.ConsumerQos:* want 1 got %d", got)
	}
	// Negation: rows where service is missing/empty.
	got, err = count(s, "-service:*")
	if err != nil {
		t.Fatal(err)
	}
	if got != 0 {
		t.Fatalf("-service:* want 0 got %d", got)
	}
	// `**` collapses to existence too.
	got, err = count(s, "service:**")
	if err != nil {
		t.Fatal(err)
	}
	if got != 4 {
		t.Fatalf("service:** want 4 got %d", got)
	}
}

// Globbed quoted strings (item 4): `*` inside quotes is a wildcard;
// `\*` escapes to a literal asterisk.
func TestQuotedGlob(t *testing.T) {
	s := setup(t)
	got, err := count(s, `msg:"*timeout*"`)
	if err != nil {
		t.Fatal(err)
	}
	if got != 1 {
		t.Fatalf(`msg:"*timeout*" want 1 got %d`, got)
	}
	// Anchored glob: prefix match.
	got, err = count(s, `msg:"dispatcher*"`)
	if err != nil {
		t.Fatal(err)
	}
	if got != 1 {
		t.Fatalf(`msg:"dispatcher*" want 1 got %d`, got)
	}
	// `\*` is a literal asterisk; no row has one in setup data.
	got, err = count(s, `msg:"\*"`)
	if err != nil {
		t.Fatal(err)
	}
	if got != 0 {
		t.Fatalf(`msg:"\*" want 0 got %d`, got)
	}
	// Multi-wild glob.
	got, err = count(s, `msg:"*kafka*retry*"`)
	if err != nil {
		t.Fatal(err)
	}
	if got != 1 {
		t.Fatalf(`msg:"*kafka*retry*" want 1 got %d`, got)
	}
}

// Bare `[ticket]` and `-->` style tokens (item 7): these tokenize as
// glob runs and become msg substrings.
func TestBareSpecialTokens(t *testing.T) {
	s := store.New(store.Options{Cap: 16})
	rows := []map[string]any{
		{"level": "info", "msg": "[TICKET-42] processing"},
		{"level": "info", "msg": "request --> response"},
		{"level": "info", "msg": "plain message"},
	}
	for _, r := range rows {
		raw, _ := json.Marshal(r)
		s.Publish(store.AppendInput{JSON: raw})
	}
	got, err := count(s, "[TICKET-42]")
	if err != nil {
		t.Fatal(err)
	}
	if got != 1 {
		t.Fatalf("[TICKET-42] want 1 got %d", got)
	}
	got, err = count(s, "-->")
	if err != nil {
		t.Fatal(err)
	}
	if got != 1 {
		t.Fatalf("--> want 1 got %d", got)
	}
	// Negation should still work — `-foo` is NOT(foo).
	got, err = count(s, "-plain")
	if err != nil {
		t.Fatal(err)
	}
	if got != 2 {
		t.Fatalf("-plain want 2 got %d", got)
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
