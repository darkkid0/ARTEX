package agent

import (
	"strings"
	"testing"

	"github.com/Autumn-27/artex/db"
)

// The operator override is only meaningful if it is rendered as its own unambiguous
// group, ordered ABOVE the boundary lists it outranks, and clearly scoped to the
// specific actions it names. These pin the rendering half; the write-side guard
// (only a human may write one) lives in addOneConstraint.
func TestRenderConstraintGroups_OverrideLeadsAndOutranks(t *testing.T) {
	out := renderConstraintGroups([]db.Constraint{
		{ID: 1, Kind: "deny", Text: "禁止对生产库做写操作"},
		{ID: 2, Kind: "allow", Text: "仅允许被动侦察"},
		{ID: 3, Kind: "override", Text: "允许导出本任务目标站点的订单数据"},
	})

	ovIdx := strings.Index(out, "【操作员明确授权")
	bIdx := strings.Index(out, "【操作约束")
	if ovIdx < 0 || bIdx < 0 {
		t.Fatalf("missing a group: override=%d boundary=%d\n%s", ovIdx, bIdx, out)
	}
	if ovIdx > bIdx {
		t.Error("授权段必须排在约束段之前——worker 自检到「这条禁止」时，例外就该在紧邻的上一段")
	}
	// The authorization must actually claim precedence, and must say it is itemized
	// rather than a blanket release (otherwise one override frees the whole task).
	if !strings.Contains(out, "凌驾于下方全部约束") {
		t.Error("授权段未声明其优先级高于下方约束")
	}
	if !strings.Contains(out, "逐条点名的例外") {
		t.Error("授权段未声明是逐条点名，模型可能理解成整体放开")
	}
	for _, want := range []string{"允许导出本任务目标站点的订单数据", "仅允许被动侦察", "禁止对生产库做写操作"} {
		if !strings.Contains(out, want) {
			t.Errorf("渲染结果丢了约束 %q", want)
		}
	}
}

// override is the only kind that must NOT be absorbed into the boundary groups —
// a silent fallback would make the operator's authorization unenforceable.
func TestRenderConstraintGroups_UnknownKindIsNotSilentlyAllowed(t *testing.T) {
	// An unrecognized kind must land in the restrictive bucket, never be dropped and
	// never be treated as an authorization.
	out := renderConstraintGroups([]db.Constraint{{ID: 1, Kind: "bogus", Text: "某条未知类型约束"}})
	if !strings.Contains(out, "某条未知类型约束") {
		t.Fatal("未知 kind 被整条丢弃了——约束必须以保守方式呈现")
	}
	// The 【操作员明确授权】group header must be absent. Note the boundary framing
	// legitimately *mentions* the group by name (it tells the model to check it), so
	// assert on the header + its "以下动作已由操作员…授权" lead-in, not the bare name.
	if strings.Contains(out, "【操作员明确授权（") {
		t.Error("未知 kind 不得产生授权段")
	}
	if strings.Contains(out, "已由操作员") {
		t.Error("未知 kind 不得被当作授权呈现")
	}
	if !strings.Contains(out, "禁止的操作") {
		t.Error("未知 kind 应落入保守（禁止）分组")
	}
}

func TestRenderConstraintGroups_EmptyInputs(t *testing.T) {
	if got := renderConstraintGroups(nil); got != "" {
		t.Errorf("nil 输入应返回空串，得到 %q", got)
	}
	if got := renderConstraintGroups([]db.Constraint{{ID: 1, Kind: "  ", Text: "   "}}); got != "" {
		t.Errorf("纯空白文本应返回空串，得到 %q", got)
	}
}

// override-only tasks are legal: an operator may unlock something on a task that has
// no allow/deny rows yet, and that must still render the boundary framing.
func TestRenderConstraintGroups_OverrideOnly(t *testing.T) {
	out := renderConstraintGroups([]db.Constraint{{ID: 1, Kind: "override", Text: "本次可以写入测试库"}})
	if !strings.Contains(out, "操作员明确授权") {
		t.Fatalf("只有授权时也应渲染授权段：\n%s", out)
	}
	if strings.Contains(out, "【操作约束") {
		t.Errorf("没有 allow/deny 时不该渲染空的约束段：\n%s", out)
	}
}
