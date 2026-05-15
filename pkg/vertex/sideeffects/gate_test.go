package sideeffects

import "testing"

func TestShouldRunSideEffect_Given_MainContext_Then_Run(t *testing.T) {
	if !ShouldRunSideEffect(Context{Mode: ModeMain}) {
		t.Error("expected side effects to run for main writes")
	}
}

func TestShouldRunSideEffect_Given_ScenarioRun_Then_Skip(t *testing.T) {
	if ShouldRunSideEffect(Context{Mode: ModeScenarioRun}) {
		t.Error("expected side effects to SKIP during scenario run")
	}
}

func TestShouldRunSideEffect_Given_ScenarioApply_Then_Run(t *testing.T) {
	if !ShouldRunSideEffect(Context{Mode: ModeScenarioApply}) {
		t.Error("expected side effects to RUN during scenario apply (real main writes)")
	}
}

func TestShouldRunSideEffect_Given_ScenarioApplyWithForceSkip_Then_Skip(t *testing.T) {
	// Operators can globally disable side effects (e.g. drill / replay).
	// The override beats the mode.
	if ShouldRunSideEffect(Context{Mode: ModeScenarioApply, ForceSkip: true}) {
		t.Error("ForceSkip should override mode")
	}
}

func TestShouldRunSideEffect_Given_UnknownMode_Then_Skip(t *testing.T) {
	// Unknown modes default to skip — safer than firing webhooks on
	// modes we have not yet evaluated.
	if ShouldRunSideEffect(Context{Mode: Mode("future")}) {
		t.Error("expected unknown mode to default to skip")
	}
}
