package guard

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Autumn-27/artex/intercept"
)

// honour_override is the one path where a deny rule stops being absolute, so its
// boundaries are pinned here: it must only ever narrow, and it must never fire
// without an actual authorization to appeal to.

// TaskAuthorizations must degrade to "no appeal" rather than panicking or allowing
// when the wiring is absent — the deny rule has to stand in that case.
func TestTaskAuthorizations_NilWiringIsSafe(t *testing.T) {
	old := TaskAuthorizations
	defer func() { TaskAuthorizations = old }()

	TaskAuthorizations = nil
	g := &Guard{}
	if got := g.taskAuthorizations(context.Background()); got != nil {
		t.Fatalf("nil wiring should yield no authorizations, got %v", got)
	}
}

// An injected lookup must be consulted and its result used as-is.
func TestTaskAuthorizations_InjectedLookupIsUsed(t *testing.T) {
	old := TaskAuthorizations
	defer func() { TaskAuthorizations = old }()

	TaskAuthorizations = func(ctx context.Context) []string {
		return []string{"允许导出订单数据"}
	}
	g := &Guard{}
	got := g.taskAuthorizations(context.Background())
	if len(got) != 1 || got[0] != "允许导出订单数据" {
		t.Fatalf("injected lookup not used: %v", got)
	}
}

// The authorizations ride the review context as their OWN trusted field. They must not
// be smuggled into background: the input-boundary contract (JudgeContextBoundary)
// forbids background from expanding authorization, so a regression here would let a
// deny rule be waived by model-adjacent text.
func TestOperatorAuthorizationsAreNotBackground(t *testing.T) {
	base := intercept.WithReviewContext(context.Background(), "/wd", intercept.ReviewBackground{
		Source: intercept.BackgroundUserMessage,
		Text:   "请删除那条记录",
	})
	ctx := intercept.WithOperatorAuthorizations(base, []string{"允许导出订单数据", "  ", "允许改状态"})

	in, err := intercept.BuildReviewInput(ctx, "Bash", json.RawMessage(`{"command":"x"}`))
	if err != nil {
		t.Fatalf("BuildReviewInput: %v", err)
	}
	if in.Background == nil || !strings.Contains(in.Background.Text, "请删除那条记录") {
		t.Fatal("escalation must preserve the original background")
	}
	want := []string{"允许导出订单数据", "允许改状态"}
	if len(in.OperatorAuthorizations) != len(want) {
		t.Fatalf("authorizations = %v, want %v (blank entries must be dropped)", in.OperatorAuthorizations, want)
	}
	for i := range want {
		if in.OperatorAuthorizations[i] != want[i] {
			t.Errorf("authorizations[%d] = %q, want %q", i, in.OperatorAuthorizations[i], want[i])
		}
	}
	if strings.Contains(in.Background.Text, "允许导出订单数据") {
		t.Error("authorizations leaked into background — background must never carry authorization")
	}

	// Serialized form must expose them as their own key so the judge prompt contract
	// can name it.
	raw, _ := json.Marshal(in)
	if !strings.Contains(string(raw), `"operator_authorizations"`) {
		t.Errorf("operator_authorizations missing from wire format: %s", raw)
	}
}

// Without the escalation the input must carry no authorizations at all, so an ordinary
// review can never accidentally see them.
func TestOperatorAuthorizationsAbsentByDefault(t *testing.T) {
	ctx := intercept.WithReviewContext(context.Background(), "/wd", intercept.ReviewBackground{})
	in, err := intercept.BuildReviewInput(ctx, "Bash", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("BuildReviewInput: %v", err)
	}
	if len(in.OperatorAuthorizations) != 0 {
		t.Fatalf("ordinary review must carry no authorizations, got %v", in.OperatorAuthorizations)
	}
}

// WithOperatorAuthorizations must not clobber working dir / background — the judge
// still needs the same view it would have had on a normal review.
func TestOperatorAuthorizationsPreserveReviewEnvironment(t *testing.T) {
	base := intercept.WithReviewContext(context.Background(), "/wd/run", intercept.ReviewBackground{
		Source: intercept.BackgroundUserMessage,
		Text:   "把这条删掉",
	})
	in, err := intercept.BuildReviewInput(intercept.WithOperatorAuthorizations(base, []string{"x"}), "Bash", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("BuildReviewInput: %v", err)
	}
	if in.WorkingDir != "/wd/run" {
		t.Errorf("working dir clobbered: %q", in.WorkingDir)
	}
	if in.Background == nil || in.Background.Text != "把这条删掉" {
		t.Errorf("background clobbered: %+v", in.Background)
	}
}
