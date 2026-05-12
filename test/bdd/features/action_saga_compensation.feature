Feature: Action Saga compensation
  As a backend engineer integrating long-running multi-step actions,
  I need the Action Saga coordinator to honour the reverse-order
  compensation contract and the dead-letter-queue contract even when
  the downstream NATS publisher misbehaves so a partial side-effect
  never lands on the wire.

  Background:
    Given the saga test ontology "saga-shop" is seeded with Order/Booking object types and compensating action types

  Scenario: Happy path commits every step and never enqueues a compensation
    When I POST applySaga to ontology "saga-shop" with these steps:
      | actionType   | primaryKey | name  | resourceId |
      | createOrder  | ord-1      | Alice |            |
      | bookResource | book-1     |       | r1         |
    Then the saga HTTP status code is 200
    And the saga response body status is "SUCCESS"
    And the saga response body has 2 applied entries
    And the saga response body has 0 compensation entries
    And the saga response body has 0 DLQ entries
    And the publisher captured 1 primary batch
    And the action_sagas row has status "SUCCESS"
    And the action_saga_steps row at index 0 has status "APPLIED"
    And the action_saga_steps row at index 1 has status "APPLIED"
    And the action_saga_dlq table has 0 PENDING rows

  Scenario: Failed step in the middle compensates earlier steps in reverse order
    When I POST applySaga to ontology "saga-shop" with these steps:
      | actionType   | primaryKey | name  | resourceId |
      | createOrder  | ord-1      | Alice |            |
      | bookResource | book-1     |       |            |
    Then the saga HTTP status code is 400
    And the saga response body status is "COMPENSATED"
    And the saga response body has 1 compensation entries
    And the saga response body has 0 DLQ entries
    And the publisher captured 1 primary batch
    And the action_sagas row has status "COMPENSATED"
    And the action_saga_steps row at index 0 has status "COMPENSATED"
    And the action_saga_steps row at index 1 has status "FAILED"
    And the action_saga_dlq table has 0 PENDING rows

  Scenario: Compensation publish failure routes the inverse batch into the DLQ
    Given the publisher will fail the next publish
    When I POST applySaga to ontology "saga-shop" with these steps:
      | actionType   | primaryKey | name  | resourceId |
      | createOrder  | ord-1      | Alice |            |
      | bookResource | book-1     |       |            |
    Then the saga HTTP status code is 400
    And the saga response body status is "FAILED"
    And the saga response body has 1 DLQ entries
    And the publisher captured 0 primary batches
    And the action_sagas row has status "FAILED"
    And the action_saga_steps row at index 0 has status "COMPENSATION_FAILED"
    And the action_saga_steps row at index 1 has status "FAILED"
    And the action_saga_dlq table has 1 PENDING row
