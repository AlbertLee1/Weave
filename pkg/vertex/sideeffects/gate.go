// Package sideeffects houses the gate that decides whether an Action's
// side effects (webhooks, emails, downstream API calls) should fire for
// a given write context.
//
// Why: when a Scenario is "Run" the edits land in scenario_edits, not in
// main — so the webhook would be lying ("look, this thing happened!"
// — no it didn't, only in the fork). Once the Scenario is Applied, the
// edits land in main for real, and side effects MUST fire.
//
// Operators can globally disable side effects via ForceSkip (e.g. during
// data drills or DR replays).
package sideeffects

// Mode tags the write context. The Action executor sets one of these per
// invocation so this gate can rule.
type Mode string

const (
	// ModeMain is the regular Action.apply / batched OSS writes on main.
	ModeMain Mode = "main"
	// ModeScenarioRun: edits land in scenario_edits, NOT in main.
	ModeScenarioRun Mode = "scenarioRun"
	// ModeScenarioApply: scenario is being applied to main → real writes.
	ModeScenarioApply Mode = "scenarioApply"
)

// Context carries the gate inputs.
type Context struct {
	Mode      Mode
	ForceSkip bool
}

// ShouldRunSideEffect returns true only for mode = main or scenarioApply,
// unless ForceSkip is set. Unknown modes default to skip — safer than
// firing on modes we have not evaluated.
func ShouldRunSideEffect(ctx Context) bool {
	if ctx.ForceSkip {
		return false
	}
	switch ctx.Mode {
	case ModeMain, ModeScenarioApply:
		return true
	default:
		return false
	}
}
