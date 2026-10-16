package cli

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/aries1980/lodo/internal/config"
	"github.com/aries1980/lodo/internal/run"
	"github.com/aries1980/lodo/internal/store"
)

// runRefresh implements lodo refresh: pull-only, reconcile plus store
// commit. It never resolves the graph and never writes the calendar.
func runRefresh(opts *options, ov configOverrides) error {
	cfgPath := opts.config
	if cfgPath == "" {
		def, err := config.DefaultConfigPath()
		if err != nil {
			return &exitError{code: codeError, err: fmt.Errorf("locate the config file: %w", err)}
		}
		cfgPath = def
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return &exitError{code: codeError, err: err}
	}
	cfg, err = config.Apply(cfg, config.Overrides{
		ReadCalendars: ov.readCalendars,
	})
	if err != nil {
		return &exitError{code: codeError, err: err}
	}
	credPath, err := config.DefaultCredentialsPath()
	if err != nil {
		return &exitError{code: codeError, err: fmt.Errorf("locate the credentials file: %w", err)}
	}
	// Refresh never touches Logseq: ReadCredentials (0600) is enough, and
	// no logseq.token is demanded.
	creds, err := config.ReadCredentials(credPath)
	if err != nil {
		return &exitError{code: codeError, err: err}
	}
	cal, err := calendarFull(context.Background(), creds, cfg)
	if err != nil {
		return &exitError{code: codeError, err: err}
	}
	st, err := openStore()
	if err != nil {
		return &exitError{code: codeError, err: err}
	}
	defer func() { _ = st.Close() }()

	now := time.Now()
	outcome, err := run.Refresh(context.Background(), cal, st, cfg, now)
	if err != nil {
		return &exitError{code: codeError, err: err}
	}

	if opts.json {
		view := refreshReport{
			Dispositions: refreshDispositions(outcome),
			Adopted:      refreshAdopted(outcome),
			Captured:     refreshCaptured(outcome),
			Busy:         busyView(outcome.Busy),
			Conflicts:    conflictsView(outcome.Conflicts),
		}
		if err := printJSON(opts.out, view); err != nil {
			return &exitError{code: codeError, err: err}
		}
		return nil
	}
	renderRefreshText(opts.out, outcome, opts.verbose)
	return nil
}

type refreshReport struct {
	Dispositions []dispositionJSON `json:"dispositions,omitempty"`
	Adopted      []bindingJSON     `json:"adopted,omitempty"`
	Captured     []dispositionJSON `json:"captured,omitempty"`
	Busy         busyJSON          `json:"busy_sources"`
	Conflicts    []conflictJSON    `json:"conflicts,omitempty"`
}

type bindingJSON struct {
	TaskID     string    `json:"task_id"`
	BlockIndex int       `json:"block_index"`
	EventID    string    `json:"event_id"`
	Start      time.Time `json:"start"`
	End        time.Time `json:"end"`
	Slots      int       `json:"slots"`
}

func refreshDispositions(outcome run.RefreshOutcome) []dispositionJSON {
	out := make([]dispositionJSON, 0, len(outcome.Dispositions))
	for _, d := range outcome.Dispositions {
		out = append(out, dispositionJSON{
			TaskID: d.TaskID, BlockIndex: d.BlockIndex, Kind: string(d.Kind),
			Start: d.Start, End: d.End, Slots: d.Slots,
		})
	}
	return out
}

func refreshAdopted(outcome run.RefreshOutcome) []bindingJSON {
	out := make([]bindingJSON, 0, len(outcome.Adopted))
	for _, b := range outcome.Adopted {
		out = append(out, bindingJSON{
			TaskID: b.TaskID, BlockIndex: b.BlockIndex, EventID: b.EventID,
			Start: b.Start, End: b.End, Slots: b.Slots,
		})
	}
	return out
}

func refreshCaptured(outcome run.RefreshOutcome) []dispositionJSON {
	out := make([]dispositionJSON, 0, len(outcome.Captured))
	for _, d := range outcome.Captured {
		out = append(out, dispositionJSON{
			TaskID: d.TaskID, BlockIndex: d.BlockIndex, Kind: string(d.Kind),
			Start: d.Start, End: d.End, Slots: d.Slots,
		})
	}
	return out
}

func renderRefreshText(w interface {
	Write([]byte) (int, error)
}, outcome run.RefreshOutcome, verbose bool) {
	printBusyText(w, outcome.Busy)
	printConflictsText(w, nil, outcome.Conflicts)
	if len(outcome.Adopted) == 0 && len(outcome.Captured) == 0 && len(outcome.Conflicts) == 0 {
		_, _ = fmt.Fprintln(w, "Refresh: local state already matches the calendar.")
		return
	}
	_, _ = fmt.Fprintf(w, "Refresh: %d binding(s) adopted, %d disposition(s) captured.\n",
		len(outcome.Adopted), len(outcome.Captured))
	if !verbose {
		return
	}
	adopted := append([]store.Binding{}, outcome.Adopted...)
	slices.SortFunc(adopted, func(a, b store.Binding) int {
		if a.TaskID != b.TaskID {
			return strings.Compare(a.TaskID, b.TaskID)
		}
		return a.BlockIndex - b.BlockIndex
	})
	for _, b := range adopted {
		_, _ = fmt.Fprintf(w, "  adopted %s → %s block %d\n", b.EventID, b.TaskID, b.BlockIndex)
	}
	for _, d := range outcome.Captured {
		_, _ = fmt.Fprintf(w, "  %s block %d: %s\n", d.TaskID, d.BlockIndex, d.Kind)
	}
}
