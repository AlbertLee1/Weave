Feature: Ontology branch merge lifecycle (US-013)
  As a schema reviewer
  I want to merge an ontology branch back into main
  So that pending schema edits become canonical only after passing conflict
  detection and the proposal approval workflow.

  Background:
    Given a fresh weave database with migrations applied
    And an ontology "bdd_merge" exists with displayName "BDD Merge"
    And an objectType "Customer" with displayName "Customer v1" exists in ontology "bdd_merge"

  Scenario: Happy path — branch with non-conflicting change merges and updates main
    Given an open branch "feature/happy" off ontology "bdd_merge"
    And the branch "feature/happy" records a MODIFIED change on "Customer" with new displayName "Customer v2"
    When I POST merge for branch "feature/happy" with no conflict resolution
    Then the merge response status is 200
    And the merge response has appliedCount 1 and skippedCount 0
    And the branch "feature/happy" has status "merged" in the database
    And the objectType "Customer" in ontology "bdd_merge" has displayName "Customer v2" in the database

  Scenario: Conflict path — main moved after branch was forked, merge rejected with 409
    Given an open branch "feature/conflict" off ontology "bdd_merge"
    And the branch "feature/conflict" records a MODIFIED change on "Customer" with before displayName "Customer v1" and new displayName "Customer branch wins"
    And main updates the objectType "Customer" displayName to "Customer main drift"
    When I POST merge for branch "feature/conflict" with no conflict resolution
    Then the merge response status is 409
    And the merge response errorCode is "MERGE_CONFLICT"
    And the merge response lists a conflict on "objectType:Customer"
    And the branch "feature/conflict" has status "open" in the database
    And the objectType "Customer" in ontology "bdd_merge" has displayName "Customer main drift" in the database

  Scenario: Approve and reject — proposal lifecycle drives merge readiness
    Given an open branch "feature/proposal" off ontology "bdd_merge"
    And the branch "feature/proposal" records a MODIFIED change on "Customer" with new displayName "Customer proposed"
    And a proposal "p1" authored by "alice" targets branch "feature/proposal" with title "Bump Customer"
    When "bob" approves proposal "p1"
    Then the proposal "p1" has status "approved" in the database
    When "carol" rejects proposal "p1"
    Then the proposal "p1" has status "rejected" in the database
    When I POST merge for proposal "p1"
    Then the proposal merge response status is 409
    And the proposal "p1" has status "rejected" in the database
    And the branch "feature/proposal" has status "open" in the database
