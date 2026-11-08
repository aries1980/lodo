Feature: Suggestions respect dependencies and Pins
  Blocked tasks suggest fixing the blocker, not moving dates.
  Pins stay at authored time.

  Scenario: Blocked Task suggests fixing blocker
    Given Task B depends on Task A which shortfalls
      And an empty write calendar
    When I run "lodo forecast --json"
    Then exit code is 0
      And Task B reports "blocked by" with blocker IDs
      And a suggestion "complete A first" is reported
      And no suggestion proposes moving the plan-end for B

  Scenario: Pin preceding dependency is never moved
    Given a Pin preceding its dependency end
      And an empty write calendar
    When I run "lodo check --json"
    Then exit code is 0
      And the Pin stays at authored time
      And no suggestion proposes moving the Pin

  Scenario: Suggestions are stable in JSON and human text
    Given a TODO "Write report" with duration "4h" and plan-end Friday
      And a Deep Work map "Mon-Fri 09:00-10:00" with 1h free
      And an empty write calendar
    When I run "lodo forecast --json" twice with same inputs
    Then suggestions are byte-identical
      And human output contains "Suggestion:" with expected impact

  Scenario: No auto-mutation
    Given a TODO "Write report" with duration "4h" and plan-end Friday
      And a Deep Work map "Mon-Fri 09:00-10:00" with 1h free
      And an empty write calendar
    When I run "lodo schedule"
    Then the second "lodo schedule" makes no calendar writes
      And Logseq was never written
