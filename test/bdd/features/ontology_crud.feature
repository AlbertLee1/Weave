Feature: Ontology CRUD lifecycle
  As a Weave platform engineer
  I want CRUD operations on Ontology rows to behave correctly against a real PostgreSQL backend
  So that the OMS metadata API contract holds end-to-end.

  Background:
    Given a fresh weave database with migrations applied

  Scenario: Create a new ontology
    When I create an ontology with apiName "bdd_create" and displayName "BDD Create Demo"
    Then the ontology "bdd_create" exists in the database
    And the ontology "bdd_create" has displayName "BDD Create Demo"
    And the ontology "bdd_create" has currentVersion 0

  Scenario: Update an existing ontology
    Given an ontology "bdd_update" exists with displayName "Old Name"
    When I update the ontology "bdd_update" displayName to "New Name"
    Then the ontology "bdd_update" has displayName "New Name"

  Scenario: Delete an ontology and verify it is gone
    Given an ontology "bdd_delete" exists with displayName "To Be Deleted"
    When I delete the ontology "bdd_delete"
    Then the ontology "bdd_delete" no longer exists in the database
