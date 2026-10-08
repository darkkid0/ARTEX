package server

import (
	"regexp"
	"testing"

	"github.com/Autumn-27/artex/agent"
	"github.com/Autumn-27/artex/traffic"
)

// Every snake_case name an agent prompt tells the model to use must actually
// resolve — either to a tool the agent can be bound to, or to a graph field /
// tool parameter explicitly listed in notTools below.
//
// A prompt naming a non-existent tool makes the model burn a turn on a call that
// can only fail. The worst case was node_detail's "未找到探索节点" error, which
// pointed the model at asset_neighbors — a tool that was never implemented (not in
// the tools table, not in this repo, not in the norma SDK), so following the advice
// guaranteed a second failure. Regression guard for that class of drift.
//
// This lives in package server (not agent) because the tool inventory is spread
// across packages and server is the only place all of them are reachable from —
// agent.BuiltinToolSeeds() alone covers just the domain tools.
func TestPromptToolNamesExist(t *testing.T) {
	tokRe := regexp.MustCompile(`[a-z][a-z0-9]*(?:_[a-z0-9]+)+`)

	prompts := map[string]string{
		"planner":   agent.BuiltinPromptSeeds()["planner"],
		"mainagent": agent.BuiltinPromptSeeds()["mainagent"],
		"worker":    agent.BuiltinPromptSeeds()["worker"],
		"auto":      agent.BuiltinPromptSeeds()["auto"],
		"pentest":   agent.BuiltinPromptSeeds()["pentest"],
		"goals":     agent.BuiltinPromptSeeds()["goals"],
		"reporter":  agent.ReporterDefaultPrompt,
		"retester":  agent.RetesterDefaultPrompt,
	}

	// Snake_case tokens that are NOT tools: graph_overview output fields, tool
	// parameters, and data keys. Keep this list explicit — adding an entry means
	// deciding something isn't a tool, so it should be argued about in review
	// rather than silently tolerated.
	notTools := map[string]bool{
		"asset_ids": true, "binding_id": true, "company_id": true, "confidence": true,
		"evidence_id": true, "evidence_version": true, "finding_id": true, "findings_total": true,
		"from_intent": true, "frontier_open": true, "goal_id": true, "has_more": true,
		"intent_id": true, "node_id": true, "parent_ids": true, "recent_done": true,
		"recent_facts": true, "running_intents": true, "sites_without_endpoints": true,
		"step_ids": true, "task_id": true, "traffic_refs": true, "user_message": true,
	}

	known := map[string]bool{}
	for _, sd := range agent.BuiltinToolSeeds() {
		known[sd.Key] = true
	}
	for _, tm := range traffic.SeedToolMetas() {
		known[tm.Name()] = true
	}
	// The server-owned tool constructors only build a value — the *Server is
	// captured inside the call handler's closure and not dereferenced until the
	// model actually calls the tool. So a nil receiver is enough to enumerate
	// their names here, and the inventory can't drift from the real wiring.
	var nilServer *Server
	for _, t := range nilServer.orchestrationTools() {
		known[t.Name()] = true
	}
	for _, t := range nilServer.platformTools() {
		known[t.Name()] = true
	}
	for _, t := range nilServer.findingRetestTools() {
		known[t.Name()] = true
	}

	for name, body := range prompts {
		seen := map[string]bool{}
		for _, tok := range tokRe.FindAllString(body, -1) {
			if seen[tok] || known[tok] || notTools[tok] {
				continue
			}
			seen[tok] = true
			t.Errorf("%s 提示词引用了 %q，但它不是任何已注册的工具（也不在 notTools 白名单里）。"+
				"模型会照着调用一个必然失败的接口，白烧一轮。", name, tok)
		}
	}
}
