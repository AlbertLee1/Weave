Feature: Automation rule lifecycle (US-015)
  As an ontology admin
  I want automation rules to honour their create → trigger → execute →
  pause/resume contract so that paused rules and rules whose condition
  evaluates to false never accumulate execution rows.

  Background:
    Given a fresh weave database with migrations applied
    And an ontology "bdd_automation" exists with displayName "BDD Automation"

  Scenario: Create-Trigger-Succeed — active rule with truthy condition records a success execution
    When I POST a new automation rule named "daily-sync" on ontology "bdd_automation" with triggerType "manual" and condition "true"
    Then the automation HTTP status code is 201
    And the automation response body has status "active"
    And the automation response body has triggerType "manual"
    And the automation rule "daily-sync" exists in the database with status "active"
    When the automation rule "daily-sync" fires with payload {"foo":"bar"}
    Then the automation rule "daily-sync" has 1 execution row in the database
    And the most recent execution of automation rule "daily-sync" has status "success"
    And the automation executions endpoint returns 1 entry for rule "daily-sync"

  Scenario: Pause-NoTrigger — paused rule drops the fire then resumes correctly
    When I POST a new automation rule named "ingest-watch" on ontology "bdd_automation" with triggerType "manual" and condition "true"
    Then the automation HTTP status code is 201
    When I POST pause on automation rule "ingest-watch"
    Then the automation HTTP status code is 200
    And the automation response body has status "paused"
    And the automation rule "ingest-watch" exists in the database with status "paused"
    When the automation rule "ingest-watch" fires with payload {"event":"insert"}
    Then the automation rule "ingest-watch" has 0 execution rows in the database
    And the automation executions endpoint returns 0 entries for rule "ingest-watch"
    When I POST resume on automation rule "ingest-watch"
    Then the automation HTTP status code is 200
    And the automation rule "ingest-watch" exists in the database with status "active"
    When the automation rule "ingest-watch" fires with payload {"event":"insert"}
    Then the automation rule "ingest-watch" has 1 execution row in the database
    And the most recent execution of automation rule "ingest-watch" has status "success"

  Scenario: ConditionFails-NoAction — active rule with falsy condition skips execution
    When I POST a new automation rule named "guarded" on ontology "bdd_automation" with triggerType "manual" and condition "false"
    Then the automation HTTP status code is 201
    And the automation response body has status "active"
    When the automation rule "guarded" fires with payload {"x":1}
    Then the automation rule "guarded" has 0 execution rows in the database
    And the automation executions endpoint returns 0 entries for rule "guarded"
    When I POST pause on automation rule "guarded"
    Then the automation HTTP status code is 200
    When I POST pause on automation rule "guarded"
    Then the automation HTTP status code is 409
    And the automation response errorName is "AlreadyPaused"
