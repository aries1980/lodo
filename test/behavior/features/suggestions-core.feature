Feature: Advisory suggestions for thin Time Maps
  Intent is never mutated; suggestions are opt-in only.
  Covers issue #3 core relief: thin map and plan-end extension.

  Scenario: Thin Time Map suggests concrete relief
    Given a TODO "Write report" with duration "4h" and plan-end Friday
      And a Deep Work map "Mon-Fri 09:00-10:00" with 1h free
      And an empty write calendar
    When I run "lodo forecast --json"
    Then exit code is 0
      And shortfall is reported with reason "not enough eligible time"
      And a suggestion is reported with reason "extend-plan-end"
      And the suggestion change mentions "move plan-end by"
      And the suggestion impact mentions "eligible"
      And no calendar writes occur
      And no Logseq writes occur

  Scenario: Plan-end extension is minimal
    Given a TODO "Write report" with duration "4h" and plan-end Friday
      And a Deep Work map "Mon-Fri 09:00-11:00" with 1h free
      And an empty write calendar
    When I run "lodo forecast --json"
    Then exit code is 0
      And shortfall is reported
      And a suggestion is reported with reason "extend-plan-end"
      And a second run produces byte-identical suggestions

  Scenario: Duration reduction is advisory only
    Given a TODO "Write report" with duration "2h" in a 30m gap
      And an empty write calendar
    When I run "lodo check --json"
    Then exit code is 0
      And shortfall is reported
      And a suggestion is reported with reason "reduce-duration"
      And no calendar writes occur
