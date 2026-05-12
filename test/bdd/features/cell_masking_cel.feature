Feature: Cell-level CEL masking decides clear vs masked per row at read time
  As a security admin
  I want CEL expressions on cell masks to gate per-row redaction
  So that callers see masked values when the predicate fires, clear values when it
  does not, and admins always bypass for incident response.

  Background:
    Given a fresh weave database with migrations applied
    And the cell-masking ontology "bdd_cellmask" is seeded with one customer object type and three rows

  Scenario: Mask-Hit — CEL predicate fires for a viewer caller and the ssn is redacted
    Given a cell mask on "bdd_cellmask" "customer" "c1" property "ssn" with strategy "REDACT" and expression 'user.roles.exists(r, r == "viewer")'
    When user "u:viewer" with roles "viewer" reads "customer" "c1" from "bdd_cellmask"
    Then the object GET HTTP status code is 200
    And the object response property "ssn" equals "***"
    And the object response property "name" equals "Alice"

  Scenario: Mask-Miss — CEL predicate does not fire for a finance caller so the ssn stays clear
    Given a cell mask on "bdd_cellmask" "customer" "c1" property "ssn" with strategy "REDACT" and expression 'user.roles.exists(r, r == "viewer")'
    When user "u:fin" with roles "finance" reads "customer" "c1" from "bdd_cellmask"
    Then the object GET HTTP status code is 200
    And the object response property "ssn" equals "111-22-3333"
    And the object response property "name" equals "Alice"

  Scenario: Admin-Bypass — admin caller sees clear value even though the expression evaluates to true
    Given a cell mask on "bdd_cellmask" "customer" "c1" property "ssn" with strategy "REDACT" and expression "true"
    When user "u:admin" with roles "admin" reads "customer" "c1" from "bdd_cellmask"
    Then the object GET HTTP status code is 200
    And the object response property "ssn" equals "111-22-3333"
    And the object response property "name" equals "Alice"
