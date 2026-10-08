package agent

import (
	"strings"

	"github.com/Autumn-27/artex/db"
)

// constraintBlock renders this task's operation constraints (task_constraints) into the
// planner/worker system prompt. Three groups, rendered in precedence order:
//
//  1. 【操作员明确授权】(kind=override) — the operator authorized something for THIS task.
//     Rendered FIRST and framed as outranking the boundary lists below, so a worker that
//     hits "this is forbidden" also has, immediately above it, "and here is what the
//     operator explicitly cleared". It is a DB fact (a row), not a prose hint.
//  2. 允许的操作 (kind=allow)
//  3. 禁止的操作 (kind=deny)
//
// Returns "" when there are no constraints (or ts is nil). Framing deliberately puts the
// operator's authorization and the declared boundary ABOVE the exploration heuristics so
// they win the tug-of-war against "chase another entry surface".
//
// Why the override group exists rather than a sentence like "human instructions override":
// planner/worker run headless and deliberately carry NO user message — both set an empty
// intercept.ReviewBackground (planner.go / worker.go) because the run-wide intent is not
// the current action. So there is no per-call authorization signal for them to test. The
// constraint table is the only channel that reaches them, which makes a row the only place
// an authorization can be a fact rather than a hope. See db.Constraint for who may write it.
func constraintBlock(ts *db.ExplorationStore) string {
	if ts == nil {
		return ""
	}
	rows, err := ts.ListConstraints()
	if err != nil {
		return ""
	}
	return renderConstraintGroups(rows)
}

// renderConstraintGroups turns constraint rows into the prompt block. Split out from
// constraintBlock so the rendering contract is testable without a database.
//
// Unknown kinds fall into the deny bucket rather than being dropped: a constraint the
// renderer cannot classify must still constrain, never silently disappear.
func renderConstraintGroups(rows []db.Constraint) string {
	var allow, deny, override []string
	for _, c := range rows {
		text := strings.TrimSpace(c.Text)
		if text == "" {
			continue
		}
		switch c.Kind {
		case "override":
			override = append(override, "- "+text)
		case "allow":
			allow = append(allow, "- "+text)
		default:
			deny = append(deny, "- "+text)
		}
	}
	if len(allow) == 0 && len(deny) == 0 && len(override) == 0 {
		return ""
	}
	var b strings.Builder

	// 授权段先出现：worker/planner 自检到"这条禁止"时，例外就在紧邻的上一段。
	if len(override) > 0 {
		b.WriteString("\n\n【操作员明确授权（最高优先级，凌驾于下方全部约束与启发式）】：\n")
		b.WriteString("以下动作已由操作员针对【本任务】明确授权，**直接执行、不要因为它们看起来违反下方的允许/禁止清单而拒绝或跳过**；也不要把它们当成新的探索方向去反复试探。\n")
		b.WriteString(strings.Join(override, "\n"))
		b.WriteString("\n（除上面这几条外，一切仍受下方约束约束——授权是逐条点名的例外，不是整体放开。）")
	}

	if len(allow) > 0 || len(deny) > 0 {
		b.WriteString("\n\n【操作约束（除上方【操作员明确授权】外最高优先级，凌驾于下方一切探索/拓面启发式；每生成一条意图、每执行一个动作前都必须先自检是否违反，违反即不得进行）】：")
		if len(allow) > 0 {
			b.WriteString("\n允许的操作：\n")
			b.WriteString(strings.Join(allow, "\n"))
		}
		if len(deny) > 0 {
			b.WriteString("\n禁止的操作：\n")
			b.WriteString(strings.Join(deny, "\n"))
		}
		b.WriteString("\n（发现约束之外的新目标/新端口/新主机，不等于获得授权：除非它落在上述允许范围内，或命中上方【操作员明确授权】，否则记为 out-of-scope 事实并跳过，不得为其派生意图或执行动作。）")
	}
	return b.String()
}
