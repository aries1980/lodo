Feature: Suggestion edge cases users learn from
  Eligibility, windows, calendar reality, and authoring errors.

  Scenario: TODO without duration suggests adding duration
    Given a TODO "Think about the garden" without duration
    When I run "lodo check --json"
    Then exit code is 0
      And "todo-missing-duration" is reported
      And no time suggestion is reported

  Scenario: Not-yet-due task gets no suggestion
    Given a TODO with plan-start beyond the scheduling window
      And an empty write calendar
    When I run "lodo forecast --json"
    Then exit code is 0
      And no shortfall is reported
      And no suggestion is reported

  Scenario: Overdue task suggests hours, not a plan move
    Given an overdue TODO with plan-end before today
      And an empty write calendar
    When I run "lodo forecast --json"
    Then exit code is 0
      And a suggestion about hours or duration is reported
      And no suggestion proposes extending plan-end

  Scenario: Busy calendar suggests adding hours
    Given a TODO "Write report" with duration "2h"
      And shared busy time covering the whole Time Map
      And an empty write calendar
    When I run "lodo forecast --json"
    Then exit code is 0
      And shortfall is reported
      And a suggestion about Time Map hours is reported
      And no suggestion proposes ignoring busy time

  Scenario: Unknown Time Map suggests fixing the map
    Given a TODO naming unknown map "nope"
      And an empty write calendar
    When I run "lodo check --json"
    Then exit code is 0
      And shortfall is reported
      And a suggestion to fix the map name is reported
      And no date-move suggestion is reported

  Scenario: Deadline without Plan teaches Plan
    Given a TODO with deadline but no plan-start or plan-end
      And an empty write calendar
    When I run "lodo check --json"
    Then exit code is 0
      And a suggestion to add plan-end is reported
