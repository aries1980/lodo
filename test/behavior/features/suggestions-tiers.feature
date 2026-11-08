Feature: Tier-aware suggestions
  Suggestions respect tier semantics: green fills first, then yellow, then red.
  Never suggest better-tier spillover than allowed.

  Scenario: Yellow spillover is suggested when allowed
    Given a project Task preferring "deep-work" with green full and yellow free
      And a default map with yellow hours free
      And an empty write calendar
    When I run "lodo check --json"
    Then exit code is 0
      And shortfall is reported
      And a suggestion is reported with reason "spill-to-default-yellow"

  Scenario: Yellow-only task never suggests green
    Given a yellow-only Task with duration "2h"
      And a default map with green hours free
      And an empty write calendar
    When I run "lodo check --json"
    Then exit code is 0
      And no suggestion mentions "spill to default green"

  Scenario: Red is a last resort
    Given a Task with green and yellow full and red free
      And an empty write calendar
    When I run "lodo forecast --json"
    Then exit code is 0
      And a suggestion with tier "red" is reported only if the Task already allows red
