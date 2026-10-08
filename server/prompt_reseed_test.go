package server

import (
	"strings"
	"testing"

	"github.com/Autumn-27/artex/agent"
)

// SeedPromptIfEmpty is first-insert-only, so a changed default body reaches an existing
// DB solely through reseedBuiltinPrompts. An agent missing from that list keeps its old
// prompt forever and nobody notices — goals did: it lost the「绝不登记 override」禁令
// while all seven sibling agents had theirs.
//
// This asserts the invariant so a new BuiltinPromptSeeds key (or a new custom agent with a
// default body) can't be added without also being seeded.
func TestReseedCoversEveryBuiltinAgent(t *testing.T) {
	targets := builtinPromptReseedTargets()
	covered := make(map[string]string, len(targets))
	for _, t2 := range targets {
		if _, dup := covered[t2.key]; dup {
			t.Errorf("reseed 列表里 %s 出现了两次", t2.key)
		}
		covered[t2.key] = t2.tmpl
		if t2.tmpl == "" {
			t.Errorf("reseed 目标 %s 的正文为空——它永远刷不进旧库", t2.key)
		}
	}

	for key := range agent.BuiltinPromptSeeds() {
		if _, ok := covered[key]; !ok {
			t.Errorf("agent %q 在 BuiltinPromptSeeds 里但不在 reseedBuiltinPrompts 目标里；"+
				"改了它的默认正文后，已有库将永远停留在旧提示词上", key)
		}
	}

	// reporter/retester 是自定义 agent，正文是独立常量，但同样需要被刷。
	for _, key := range []string{"reporter", "retester"} {
		if _, ok := covered[key]; !ok {
			t.Errorf("自定义 agent %q 缺少 reseed 目标", key)
		}
	}
}

// goals 抽取的是 allow/deny，不是授权。它必须明确拒绝 override——它是自动抽取者，
// 无权给自己发授权（写入侧 addOneConstraint 也会拒，但提示词要先讲清，否则模型会
// 白跑一次注定失败的工具调用）。
func TestGoalsPromptForbidsOverride(t *testing.T) {
	goals := agent.BuiltinPromptSeeds()["goals"]
	if goals == "" {
		t.Fatal("goals 默认正文为空")
	}
	if !strings.Contains(goals, "绝不登记 override") {
		t.Error("goals 提示词缺少「绝不登记 override」禁令")
	}
	// 这条禁令必须与另两条约束抽取规则并列，不能落进后面的「目标 = …」章节。
	// 参照物：紧跟其后的过渡句「登记完约束（如有）后，再进行下面的目标拆分。」
	banIdx := strings.Index(goals, "绝不登记 override")
	handOffIdx := strings.Index(goals, "登记完约束（如有）后")
	if banIdx < 0 || handOffIdx < 0 {
		t.Fatal("找不到用于定位的过渡句")
	}
	if banIdx > handOffIdx {
		t.Error("override 禁令落在了过渡句之后，会被 Markdown 渲染进「目标 = …」章节，" +
			"读起来像是拆分原则的一部分而非约束抽取阶段的注意事项")
	}
}
