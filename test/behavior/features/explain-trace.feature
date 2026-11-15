Feature: Explain trace reflects placement decisions
  `explain --json` is the user-facing view of the planner's decision trace.
  These scenarios pin the contract of each terminal state: eligibility,
  ordering, dependency gate and outcome must all describe the same decision.

  Scenario: A placed flexible Task
    Given a TODO "Write brief" with duration "1h" in a wide default map
    When I run "lodo explain --json" for task "t-placed-uuid"
    Then exit code is 0
      And the explain eligibility is considered
      And the explain ordering kind is "flexible"
      And the explain ordering tie-break is present
      And the explain ordering total is 1
      And the explain dependency is not blocked
      And the explain outcome reason is empty
      And the explain outcome shortfall is 0
      And the explain time maps have passes
      And the explain final blocks are at least 1

  Scenario: A container is never scheduled
    Given a parent TODO with a Task child in a wide default map
    When I run "lodo explain --json" for task "t-container-uuid"
    Then exit code is 0
      And the explain eligibility is not considered
      And the explain eligibility marks container
      And the explain skip reason is "container: never scheduled"
      And the explain ordering kind is "container"
      And the explain ordering total is 1

  Scenario: A dependency cycle is explained
    Given two TODOs that depend on each other
    When I run "lodo explain --json" for task "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
    Then exit code is 0
      And the explain eligibility is not considered
      And the explain ordering kind is "cycle-blocked"
      And the explain ordering tie-break is "dependency-cycle"
      And the explain ordering total is 0
      And the explain dependency is blocked
      And the explain dependency marks a cycle member
      And the explain dependency reason mentions "dependency cycle"
      And the explain outcome reason mentions "dependency cycle"

  Scenario: An unknown dependency blocks and names the reference
    Given a TODO depending on a ghost block
    When I run "lodo explain --json" for task "t-unknown-dep-uuid"
    Then exit code is 0
      And the explain eligibility is not considered
      And the explain ordering kind is "blocked"
      And the explain ordering total is 0
      And the explain dependency is blocked
      And the explain dependency reason mentions "unknown dependency"
      And the explain dependency names blocker "99999999-9999-4999-8999-999999999999"
      And the explain outcome reason mentions "unknown dependency"

  Scenario: A dependent is blocked by its shortfalling dependency
    Given Task B depends on Task A which shortfalls
    When I run "lodo explain --json" for task "22222222-2222-4222-8222-222222222222"
    Then exit code is 0
      And the explain eligibility is considered
      And the explain ordering kind is "time-bound"
      And the explain ordering total is 2
      And the explain dependency is blocked
      And the explain dependency reason mentions "blocked by incomplete dependencies"
      And the explain dependency names blocker "11111111-1111-4111-8111-111111111111"
      And the explain outcome reason mentions "blocked by incomplete dependencies"

  Scenario: A partially placed Task explains the shortfall
    Given a TODO "Big task" with duration "8h" in a narrow default map
    When I run "lodo explain --json" for task "t-partial-uuid"
    Then exit code is 0
      And the explain eligibility is considered
      And the explain ordering kind is "flexible"
      And the explain dependency is not blocked
      And the explain outcome reason mentions "not enough eligible time"
      And the explain outcome shortfall is greater than 0
      And the explain time maps have passes

  Scenario: A Task with no eligible time
    Given a TODO whose default map has no hours
    When I run "lodo explain --json" for task "t-none-uuid"
    Then exit code is 0
      And the explain eligibility is considered
      And the explain ordering kind is "flexible"
      And the explain dependency is not blocked
      And the explain outcome reason mentions "no eligible time"
      And the explain outcome shortfall is 4
      And the explain time maps have passes

  Scenario: A Task beyond the horizon is left unplaced silently
    Given a TODO with plan-start beyond the scheduling window
    When I run "lodo explain --json" for task "t-future-uuid"
    Then exit code is 0
      And the explain eligibility is not considered
      And the explain eligibility marks beyond horizon
      And the explain ordering kind is "time-bound"
      And the explain ordering tie-break is "beyond-horizon"
      And the explain ordering total is 1
      And the explain dependency is not blocked

  Scenario: A Pin that precedes its dependency is reported
    Given a Pin preceding its dependency end
    When I run "lodo explain --json" for task "22222222-2222-4222-8222-222222222222"
    Then exit code is 0
      And the explain eligibility is considered
      And the explain eligibility marks pin
      And the explain ordering kind is "pin"
      And the explain ordering total is 1
      And the explain dependency is blocked
      And the explain dependency marks a pin violation
      And the explain dependency reason mentions "pin precedes dependency"
      And the explain outcome reason mentions "pin precedes dependency"
