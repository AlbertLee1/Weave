Feature: Function version binding and deterministic replay
  As a Function registry caller
  I want each published Function version to be addressable by either an
  explicit `name@version` pin or the bare name (resolving to the latest
  semver), and I want POST /replay to re-execute an earlier invocation
  against the exact version that was bound at the time the original
  execution row was persisted
  So that audit + RC pinning workflows in OS v2 see byte-identical
  outputs across re-runs and never accidentally drift onto a newer
  build.

  Background:
    Given a fresh weave database with migrations applied
    And the function ontology "bdd_funcrepo" has two published versions of "compute" returning "alpha-v1" at "1.0.0" and "beta-v2" at "2.0.0"

  Scenario: Replay-Same-Result — replay the recorded execution and assert byte-identical output
    When the operator executes function "compute@1.0.0" in "bdd_funcrepo" with input '{"x":1}'
    And the operator replays the recorded execution for "compute" in "bdd_funcrepo"
    Then the function HTTP status code is 200
    And the function response field "match" is true
    And the function response field "functionVersion" is "1.0.0"
    And the function response field "originalHash" equals the field "replayHash"
    And the function execution store has 2 rows for "compute" version "1.0.0"
    And the function execution store has 1 replay row pointing at the original execution

  Scenario: Pin-Old-Version — replay a historical execution after a newer version is published
    When the operator executes function "compute@1.0.0" in "bdd_funcrepo" with input '{"x":1}'
    And the operator publishes a new version of "compute" "3.0.0" returning "gamma-v3" in "bdd_funcrepo"
    And the operator replays the recorded execution for "compute" in "bdd_funcrepo"
    Then the function HTTP status code is 200
    And the function response field "match" is true
    And the function response field "functionVersion" is "1.0.0"
    And the function execution store row 1 has version "1.0.0" and is a replay

  Scenario: Default-Latest — bare function name resolves to the highest semver
    When the operator executes function "compute" in "bdd_funcrepo" with input '{"x":1}'
    Then the function HTTP status code is 200
    And the function response field "functionRid" matches the latest version of "compute" in "bdd_funcrepo"
    And the function execution store row 0 has version "2.0.0" and is not a replay
