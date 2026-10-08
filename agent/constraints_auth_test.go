package agent

import (
	"strings"
	"testing"
)

// The authorization override is worthless if the model can mint one for itself — it
// would just self-authorize out of every boundary. Only the main agent transcribing an
// operator instruction (t.worker == "human") may write one; the goals decomposer and the
// workers must be refused.
func TestAddOneConstraint_OverrideRequiresHumanOrigin(t *testing.T) {
	refused := []string{"goals", "planner", "worker work#1", "orchestrator", ""}
	for _, worker := range refused {
		ts := &ToolSet{worker: worker}
		_, err := ts.addOneConstraint(constraintItem{Text: "允许导出订单数据", Type: "override"})
		if err == nil {
			t.Errorf("worker=%q 竟成功登记了 override——模型可以给自己发授权，豁免形同虚设", worker)
			continue
		}
		if !strings.Contains(err.Error(), "override") {
			t.Errorf("worker=%q 的报错应说明 override 的限制，实际：%v", worker, err)
		}
	}
}

func TestAddOneConstraint_AllowDenyStillOpenToEveryone(t *testing.T) {
	// Regression guard on the guard: rejecting override must not accidentally reject the
	// boundary kinds the goals decomposer legitimately writes.
	//
	// Validation happens in addOneConstraint before it ever touches the store, so reaching
	// AddConstraint at all (it panics on our nil store) is the proof that validation
	// passed. Recover per case so one panic doesn't abort the whole test.
	for _, worker := range []string{"goals", "planner", "worker work#1"} {
		for _, kind := range []string{"allow", "deny"} {
			if err := reachedStore(worker, kind); err != nil {
				t.Errorf("worker=%q 的 %s 未通过校验就被拒：%v", worker, kind, err)
			}
		}
	}
}

// reachedStore runs addOneConstraint for one (worker, kind) and reports whether it got
// past validation. A nil-store panic means it did.
func reachedStore(worker, kind string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = nil // reached db.AddConstraint with a nil store → validation passed
		}
	}()
	ts := &ToolSet{ts: nil, worker: worker}
	_, err = ts.addOneConstraint(constraintItem{Text: "x", Type: kind})
	return err
}

func TestAddOneConstraint_RejectsUnknownKind(t *testing.T) {
	ts := &ToolSet{worker: "human"}
	for _, kind := range []string{"bogus", "ALLOW_MAYBE", "over ride"} {
		if _, err := ts.addOneConstraint(constraintItem{Text: "x", Type: kind}); err == nil {
			t.Errorf("未知 type=%q 未被拒绝", kind)
		}
	}
}
