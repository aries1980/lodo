Feature: TUI edits Logseq blocks on explicit save
  Bare lodo opens the TUI only on an interactive terminal; edits are full
  Logseq blocks validated per-Task before any update_block write.

  Scenario: Bare lodo shows help without a terminal
    Given a graph with one Task
    When I run bare "lodo" without a TTY
    Then exit code is 0
      And no Logseq writes occur
      And output shows the CLI help

  Scenario: --json never launches the TUI
    Given a graph with one Task
    When I run bare "lodo --json"
    Then exit code is 0
      And no Logseq writes occur
      And output shows the CLI help

  Scenario: Valid edit saves through update_block
    Given a TODO "Write report" with duration "30m"
    When I save an edit changing duration to "45m"
    Then Logseq received one update_block
      And no calendar writes occur

  Scenario: Bad duration blocks the save
    Given a TODO "Write report" with duration "30m"
    When I save an edit with duration "banana"
    Then validation reports "bad-property-value"
      And no Logseq writes occur

  Scenario: Unknown time-map blocks, dropdown patch fixes
    Given a TODO "Write report" with duration "30m" and time-map "nope"
    When I save an edit with time-map "nope"
    Then validation reports "unknown-time-map"
      And no Logseq writes occur
    When I apply the time-map dropdown fix "deep-work"
    Then the edit validates
      And saving writes one update_block

  Scenario: View preserves raw markers
    Given a TODO "**Write** report" with duration "30m"
    Then highlighted view still contains "**" and "::"
