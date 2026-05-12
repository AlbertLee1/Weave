Feature: asOf time-travel queries pin loadObjects to a historical dataset transaction
  As an analyst
  I want ?asOf=tx-<id> on the loadObjects endpoint to resolve a past
  dataset_transactions commit into the validity-window scan
  So that I can read the exact state of every object that was current
  when that transaction landed, get a clean 400 envelope when the tx
  reference is unknown, and an empty result set when no object existed
  yet at that instant.

  Background:
    Given a fresh weave database with migrations applied
    And the time-travel ontology "bdd_timetravel" is seeded with an employee object type

  Scenario: asOf-Old-Tx — pinning to an older tx returns the historical state, not the live row
    Given dataset transaction "tx-tt-001" on "bdd_timetravel" committed at "2026-01-15T00:00:00Z"
    And dataset transaction "tx-tt-002" on "bdd_timetravel" committed at "2026-02-15T00:00:00Z" with parent "tx-tt-001"
    And object history for "bdd_timetravel" "employee" "emp-1" recorded at "2026-01-15T00:00:00Z" version 1 with new state {"name":"Alice","title":"Engineer"} tx "tx-tt-001"
    And object history for "bdd_timetravel" "employee" "emp-1" recorded at "2026-02-15T00:00:00Z" version 2 with new state {"name":"Alice","title":"Staff Engineer"} tx "tx-tt-002"
    When the analyst loads objects of type "employee" from "bdd_timetravel" with asOf "tx-tt-001"
    Then the loadObjects HTTP status code is 200
    And the loadObjects totalCount is "1"
    And the loadObjects data row 0 property "name" equals "Alice"
    And the loadObjects data row 0 property "title" equals "Engineer"

  Scenario: asOf-Future-Reject — pinning to an unknown tx surfaces TransactionNotFound 400
    When the analyst loads objects of type "employee" from "bdd_timetravel" with asOf "tx-tt-future-9999"
    Then the loadObjects HTTP status code is 400
    And the loadObjects error name is "TransactionNotFound"
    And the loadObjects error parameter "txId" equals "tx-tt-future-9999"

  Scenario: asOf-NoRecord — pinning before the object existed returns an empty page
    Given dataset transaction "tx-tt-010" on "bdd_timetravel" committed at "2026-03-01T00:00:00Z"
    And object history for "bdd_timetravel" "employee" "emp-9" recorded at "2026-04-01T00:00:00Z" version 1 with new state {"name":"Bob","title":"Analyst"} tx "tx-tt-010-noop"
    When the analyst loads objects of type "employee" from "bdd_timetravel" with asOf "tx-tt-010"
    Then the loadObjects HTTP status code is 200
    And the loadObjects totalCount is "0"
    And the loadObjects data row count is 0
