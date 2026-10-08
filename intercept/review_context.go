package intercept

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

const reviewTextLimit = 4000

const BackgroundUserMessage = "user_message"

// ReviewBackground is explicitly bound from the current human message. Generated
// Worker summaries are not accepted. Background cannot override review policy.
type ReviewBackground struct {
	Source    string `json:"source"`
	Text      string `json:"text"`
	Truncated bool   `json:"truncated,omitempty"`
}

// ReviewInput contains only the current call and explicitly selected background.
// Execution history and call correlation belong to the separate audit record.
type ReviewInput struct {
	Version    int               `json:"version"`
	WorkingDir string            `json:"working_directory,omitempty"`
	Background *ReviewBackground `json:"background,omitempty"`
	// OperatorAuthorizations lists the operator's explicit authorizations recorded for
	// this task (task_constraints rows with kind='override', writable only by a human —
	// see db.Constraint). Populated ONLY when a matched rule has honor_override=true,
	// and it is the single channel by which a deny rule may be waived.
	//
	// Deliberately NOT smuggled through Background: the input-boundary contract
	// (JudgeContextBoundary) forbids background from expanding authorization, because
	// background is model-adjacent free text. This field is a DB-backed platform fact,
	// so it gets its own explicitly-trusted slot.
	OperatorAuthorizations []string        `json:"operator_authorizations,omitempty"`
	Tool                   string          `json:"tool_name"`
	Arguments              json.RawMessage `json:"arguments"`
}

type reviewContextKey struct{}
type reviewEnvironment struct {
	workingDir string
	background ReviewBackground
	// operatorAuthorizations rides the same context but is set only for the one
	// honour-override escalation, never for ordinary reviews.
	operatorAuthorizations []string
}

// WithOperatorAuthorizations attaches the task's operator authorizations for a single
// honour-override review. It does not touch workingDir/background, so escalating a deny
// rule to the judge still shows the judge exactly the same background as a normal review.
func WithOperatorAuthorizations(ctx context.Context, auths []string) context.Context {
	env, _ := ctx.Value(reviewContextKey{}).(reviewEnvironment)
	env.operatorAuthorizations = auths
	return context.WithValue(ctx, reviewContextKey{}, env)
}

// WithReviewContext explicitly binds the permitted background for one run. Never
// fall back to the raw turn transcript: it may contain the full scheduler prompt.
// This is application wiring, not a model-callable tool.
func WithReviewContext(ctx context.Context, workingDir string, background ReviewBackground) context.Context {
	return context.WithValue(ctx, reviewContextKey{}, reviewEnvironment{
		workingDir: workingDir,
		background: background,
	})
}

// WithReviewWorkingDirectory preserves only explicitly selected background.
// Chat runs can be human-initiated or scheduled, so the Agent must not infer
// message provenance from the text it receives.
func WithReviewWorkingDirectory(ctx context.Context, workingDir string) context.Context {
	env, _ := ctx.Value(reviewContextKey{}).(reviewEnvironment)
	env.workingDir = workingDir
	return context.WithValue(ctx, reviewContextKey{}, env)
}

func BuildReviewInput(ctx context.Context, tool string, arguments json.RawMessage) (ReviewInput, error) {
	if !json.Valid(arguments) {
		return ReviewInput{}, fmt.Errorf("工具参数不是有效 JSON")
	}
	in := ReviewInput{Version: 5, Tool: tool, Arguments: append(json.RawMessage(nil), arguments...)}
	if env, ok := ctx.Value(reviewContextKey{}).(reviewEnvironment); ok {
		in.WorkingDir = env.workingDir
		background := env.background
		if background.Source == BackgroundUserMessage && strings.TrimSpace(background.Text) != "" {
			var cut bool
			background.Text, cut = bounded(background.Text, reviewTextLimit)
			background.Truncated = background.Truncated || cut
			in.Background = &background
		}
		for _, a := range env.operatorAuthorizations {
			if a = strings.TrimSpace(a); a != "" {
				in.OperatorAuthorizations = append(in.OperatorAuthorizations, a)
			}
		}
	}
	return in, nil
}
