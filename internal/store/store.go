// Package store is the system of record (ADR 0001): Task→Block→event-id
// bindings plus input snapshots; the calendar is a projection.
//
// SQLite at ${XDG_STATE_HOME:-~/.local/state}/lodo/lodo.db, opened per run;
// there is no daemon (ADR 0009/CLI). Tables are created if not exists with
// no migration framework in v0.1: if the schema must change later,
// drop-and-adopt is acceptable because the calendar carries the identities.
package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	_ "modernc.org/sqlite"

	"github.com/aries1980/lodo/internal/model"
)

// Binding is one projected Block: the Task→Block→event-id row.
type Binding struct {
	TaskID     string
	BlockIndex int
	EventID    string
	Start      time.Time
	End        time.Time
	Slots      int
}

// BindingKey identifies a binding row for deletion.
type BindingKey struct {
	TaskID     string
	BlockIndex int
}

// TimedDisposition is a disposition row plus its recording time.
// RecordedAt is injected (no wall clock in the store); the caller passes
// the run's Now.
type TimedDisposition struct {
	Disposition model.Disposition
	RecordedAt  time.Time
}

// Store wraps one SQLite file.
type Store struct {
	db   *sql.DB
	path string
}

// Open creates the parent directory if needed and opens the SQLite file at
// path, creating the tables when they do not exist.
func Open(path string) (*Store, error) {
	if path == "" {
		return nil, fmt.Errorf("store: no path")
	}
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("store directory %s: %w", dir, err)
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("store %s: %w", path, err)
	}
	s := &Store{db: db, path: path}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Close releases the file.
func (s *Store) Close() error { return s.db.Close() }

// migrate creates the v0.1 tables when they do not exist. There is no
// migration framework: schema changes mean drop-and-adopt, acceptable
// because the calendar carries the identities.
func (s *Store) migrate() error {
	const schema = `
CREATE TABLE IF NOT EXISTS bindings (
  task_id TEXT NOT NULL,
  block_index INTEGER NOT NULL,
  event_id TEXT NOT NULL UNIQUE,
  start TEXT NOT NULL,
  end TEXT NOT NULL,
  slots INTEGER NOT NULL,
  PRIMARY KEY (task_id, block_index)
);
CREATE TABLE IF NOT EXISTS tasks_snapshot (
  task_id TEXT PRIMARY KEY,
  snapshot_json TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS dispositions (
  task_id TEXT NOT NULL,
  block_index INTEGER NOT NULL,
  kind TEXT NOT NULL,
  start TEXT NOT NULL DEFAULT '',
  end TEXT NOT NULL DEFAULT '',
  slots INTEGER NOT NULL DEFAULT 0,
  snapshot_json TEXT NOT NULL DEFAULT '',
  recorded_at TEXT NOT NULL,
  PRIMARY KEY (task_id, block_index)
);
CREATE TABLE IF NOT EXISTS runs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  recorded_at TEXT NOT NULL,
  command TEXT NOT NULL,
  write_calendar TEXT NOT NULL,
  write_events INTEGER NOT NULL DEFAULT 0,
  sources_json TEXT NOT NULL
);`
	if _, err := s.db.Exec(schema); err != nil {
		return fmt.Errorf("store %s: migrate: %w", s.path, err)
	}
	return nil
}

// RunSource is one read calendar's audit row for the run record.
type RunSource struct {
	CalendarID string `json:"calendar_id"`
	Status     string `json:"status"`
	Events     int    `json:"events"`
}

// RunRecord is one schedule/refresh run's calendar audit: the write calendar
// plus per-read-calendar status and counts. Forecast stays store-free by
// design and never records.
type RunRecord struct {
	ID            int64
	RecordedAt    time.Time
	Command       string // schedule or refresh
	WriteCalendar string
	WriteEvents   int
	Sources       []RunSource
}

// RecordRun appends one run's calendar audit. Sources stay in config order.
func (s *Store) RecordRun(command, writeCalendar string, writeEvents int, sources []RunSource, recordedAt time.Time) error {
	raw, err := json.Marshal(sources)
	if err != nil {
		return fmt.Errorf("store: run sources: %w", err)
	}
	if sources == nil {
		raw = []byte("[]")
	}
	if _, err := s.db.Exec(
		`INSERT INTO runs(recorded_at, command, write_calendar, write_events, sources_json) VALUES(?,?,?,?,?)`,
		recordedAt.UTC().Format(time.RFC3339Nano), command, writeCalendar, writeEvents, string(raw),
	); err != nil {
		return fmt.Errorf("store: record run: %w", err)
	}
	return nil
}

// LastRun returns the most recent run record, or an error when none exists.
func (s *Store) LastRun() (RunRecord, error) {
	row := s.db.QueryRow(`SELECT id, recorded_at, command, write_calendar, write_events, sources_json FROM runs ORDER BY id DESC LIMIT 1`)
	var rec RunRecord
	var recordedS, sourcesRaw string
	if err := row.Scan(&rec.ID, &recordedS, &rec.Command, &rec.WriteCalendar, &rec.WriteEvents, &sourcesRaw); err != nil {
		return RunRecord{}, fmt.Errorf("store: last run: %w", err)
	}
	recordedAt, err := time.Parse(time.RFC3339Nano, recordedS)
	if err != nil {
		return RunRecord{}, fmt.Errorf("store: run recorded_at: %w", err)
	}
	rec.RecordedAt = recordedAt
	if err := json.Unmarshal([]byte(sourcesRaw), &rec.Sources); err != nil {
		return RunRecord{}, fmt.Errorf("store: run sources: %w", err)
	}
	return rec, nil
}

// All loads every row: bindings, task snapshots and dispositions.
// Dispositions convert dumbly to []model.Disposition (M2 shape).
func (s *Store) All() (bindings []Binding, snapshots map[string]model.Snapshot, dispositions []model.Disposition, err error) {
	bindings, err = s.ListBindings()
	if err != nil {
		return nil, nil, nil, err
	}
	snapshots, err = s.Snapshots()
	if err != nil {
		return nil, nil, nil, err
	}
	dispositions, err = s.ListDispositions()
	if err != nil {
		return nil, nil, nil, err
	}
	return bindings, snapshots, dispositions, nil
}

// ListBindings returns every binding row ordered for determinism.
func (s *Store) ListBindings() ([]Binding, error) {
	rows, err := s.db.Query(`SELECT task_id, block_index, event_id, start, end, slots FROM bindings ORDER BY task_id, block_index`)
	if err != nil {
		return nil, fmt.Errorf("store: list bindings: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Binding
	for rows.Next() {
		var b Binding
		var startS, endS string
		if err := rows.Scan(&b.TaskID, &b.BlockIndex, &b.EventID, &startS, &endS, &b.Slots); err != nil {
			return nil, fmt.Errorf("store: scan binding: %w", err)
		}
		start, err := time.Parse(time.RFC3339Nano, startS)
		if err != nil {
			return nil, fmt.Errorf("store: binding %s/%d start: %w", b.TaskID, b.BlockIndex, err)
		}
		end, err := time.Parse(time.RFC3339Nano, endS)
		if err != nil {
			return nil, fmt.Errorf("store: binding %s/%d end: %w", b.TaskID, b.BlockIndex, err)
		}
		b.Start, b.End = start, end
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list bindings: %w", err)
	}
	return out, nil
}

// Snapshots returns the last fully-resolved inputs by task id. Only a
// schedule run's store commit writes these; forecast stays read-only and
// refresh cannot resolve the graph.
func (s *Store) Snapshots() (map[string]model.Snapshot, error) {
	rows, err := s.db.Query(`SELECT task_id, snapshot_json FROM tasks_snapshot ORDER BY task_id`)
	if err != nil {
		return nil, fmt.Errorf("store: list snapshots: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]model.Snapshot{}
	for rows.Next() {
		var id, raw string
		if err := rows.Scan(&id, &raw); err != nil {
			return nil, fmt.Errorf("store: scan snapshot: %w", err)
		}
		snap, err := SnapshotFromJSON(raw)
		if err != nil {
			return nil, fmt.Errorf("store: snapshot %s: %w", id, err)
		}
		out[id] = snap
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list snapshots: %w", err)
	}
	return out, nil
}

// ListDispositions returns every disposition ordered for determinism.
func (s *Store) ListDispositions() ([]model.Disposition, error) {
	rows, err := s.db.Query(`SELECT task_id, block_index, kind, start, end, slots, snapshot_json, recorded_at FROM dispositions ORDER BY task_id, block_index`)
	if err != nil {
		return nil, fmt.Errorf("store: list dispositions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []model.Disposition
	for rows.Next() {
		var taskID, kind, startS, endS, snapRaw, recordedS string
		var blockIndex, slots int
		if err := rows.Scan(&taskID, &blockIndex, &kind, &startS, &endS, &slots, &snapRaw, &recordedS); err != nil {
			return nil, fmt.Errorf("store: scan disposition: %w", err)
		}
		d := model.Disposition{TaskID: taskID, BlockIndex: blockIndex, Kind: model.DispositionKind(kind), Slots: slots}
		if startS != "" {
			start, err := time.Parse(time.RFC3339Nano, startS)
			if err != nil {
				return nil, fmt.Errorf("store: disposition %s/%d start: %w", taskID, blockIndex, err)
			}
			d.Start = start
		}
		if endS != "" {
			end, err := time.Parse(time.RFC3339Nano, endS)
			if err != nil {
				return nil, fmt.Errorf("store: disposition %s/%d end: %w", taskID, blockIndex, err)
			}
			d.End = end
		}
		if snapRaw != "" {
			snap, err := SnapshotFromJSON(snapRaw)
			if err != nil {
				return nil, fmt.Errorf("store: disposition %s/%d snapshot: %w", taskID, blockIndex, err)
			}
			d.Snapshot = snap
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list dispositions: %w", err)
	}
	return out, nil
}

// Commit applies one run's store writes atomically: binding upserts and
// deletes, disposition upserts, and refreshed task snapshots. The caller
// writes the calendar first and commits second (ADR 0007); a crash between
// the two leaves lodo=true events with no local row, which the next run
// adopts.
func (s *Store) Commit(bindingUpserts []Binding, bindingDeletes []BindingKey, dispositions []TimedDisposition, snapshots map[string]model.Snapshot) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("store: commit: %w", err)
	}
	rollback := func() { _ = tx.Rollback() }

	for _, b := range bindingUpserts {
		if _, err := tx.Exec(
			`INSERT INTO bindings(task_id, block_index, event_id, start, end, slots) VALUES(?,?,?,?,?,?)
			 ON CONFLICT(task_id, block_index) DO UPDATE SET event_id=excluded.event_id, start=excluded.start, end=excluded.end, slots=excluded.slots`,
			b.TaskID, b.BlockIndex, b.EventID, b.Start.UTC().Format(time.RFC3339Nano), b.End.UTC().Format(time.RFC3339Nano), b.Slots,
		); err != nil {
			rollback()
			return fmt.Errorf("store: upsert binding %s/%d: %w", b.TaskID, b.BlockIndex, err)
		}
	}
	for _, k := range bindingDeletes {
		if _, err := tx.Exec(`DELETE FROM bindings WHERE task_id=? AND block_index=?`, k.TaskID, k.BlockIndex); err != nil {
			rollback()
			return fmt.Errorf("store: delete binding %s/%d: %w", k.TaskID, k.BlockIndex, err)
		}
	}
	for _, td := range dispositions {
		d := td.Disposition
		startS, endS := "", ""
		if !d.Start.IsZero() {
			startS = d.Start.UTC().Format(time.RFC3339Nano)
		}
		if !d.End.IsZero() {
			endS = d.End.UTC().Format(time.RFC3339Nano)
		}
		snapRaw, err := SnapshotToJSON(d.Snapshot)
		if err != nil {
			rollback()
			return fmt.Errorf("store: disposition %s/%d snapshot: %w", d.TaskID, d.BlockIndex, err)
		}
		if _, err := tx.Exec(
			`INSERT INTO dispositions(task_id, block_index, kind, start, end, slots, snapshot_json, recorded_at) VALUES(?,?,?,?,?,?,?,?)
			 ON CONFLICT(task_id, block_index) DO UPDATE SET kind=excluded.kind, start=excluded.start, end=excluded.end, slots=excluded.slots, snapshot_json=excluded.snapshot_json, recorded_at=excluded.recorded_at`,
			d.TaskID, d.BlockIndex, string(d.Kind), startS, endS, d.Slots, snapRaw, td.RecordedAt.UTC().Format(time.RFC3339Nano),
		); err != nil {
			rollback()
			return fmt.Errorf("store: upsert disposition %s/%d: %w", d.TaskID, d.BlockIndex, err)
		}
	}
	for id, snap := range snapshots {
		raw, err := SnapshotToJSON(snap)
		if err != nil {
			rollback()
			return fmt.Errorf("store: snapshot %s: %w", id, err)
		}
		if _, err := tx.Exec(
			`INSERT INTO tasks_snapshot(task_id, snapshot_json) VALUES(?,?)
			 ON CONFLICT(task_id) DO UPDATE SET snapshot_json=excluded.snapshot_json`,
			id, raw,
		); err != nil {
			rollback()
			return fmt.Errorf("store: upsert snapshot %s: %w", id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit: %w", err)
	}
	return nil
}

// snapshotJSON is M2's model.Snapshot marshalled from a struct for
// deterministic field order. No omitempty: nil pointers and null slices
// round-trip as null so empty-vs-missing stays faithful.
type snapshotJSON struct {
	Text      string          `json:"text"`
	Marker    model.Marker    `json:"marker"`
	Duration  int64           `json:"duration_ns"`
	TimeMaps  []string        `json:"time_maps"`
	Priority  model.Priority  `json:"priority"`
	Deadline  *civilDateJSON  `json:"deadline"`
	PlanStart *civilDateJSON  `json:"plan_start"`
	PlanEnd   *civilDateJSON  `json:"plan_end"`
	Pin       *time.Time      `json:"pin"`
	Depends   []string        `json:"depends"`
	Order     model.OrderMode `json:"order"`
}

type civilDateJSON struct {
	Year  int        `json:"year"`
	Month time.Month `json:"month"`
	Day   int        `json:"day"`
}

// SnapshotToJSON marshals one snapshot with deterministic field order.
func SnapshotToJSON(s model.Snapshot) (string, error) {
	dto := snapshotJSON{
		Text:     s.Text,
		Marker:   s.Marker,
		Duration: int64(s.Duration),
		TimeMaps: s.TimeMaps,
		Priority: s.Priority,
		Pin:      s.Pin,
		Depends:  s.Depends,
		Order:    s.Order,
	}
	dto.Deadline = civilToJSON(s.Deadline)
	dto.PlanStart = civilToJSON(s.PlanStart)
	dto.PlanEnd = civilToJSON(s.PlanEnd)
	raw, err := json.Marshal(dto)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// SnapshotFromJSON parses one snapshot.
func SnapshotFromJSON(raw string) (model.Snapshot, error) {
	var dto snapshotJSON
	if err := json.Unmarshal([]byte(raw), &dto); err != nil {
		return model.Snapshot{}, err
	}
	out := model.Snapshot{
		Text:     dto.Text,
		Marker:   dto.Marker,
		Duration: time.Duration(dto.Duration),
		TimeMaps: dto.TimeMaps,
		Priority: dto.Priority,
		Pin:      dto.Pin,
		Depends:  dto.Depends,
		Order:    dto.Order,
	}
	out.Deadline = civilFromJSON(dto.Deadline)
	out.PlanStart = civilFromJSON(dto.PlanStart)
	out.PlanEnd = civilFromJSON(dto.PlanEnd)
	return out, nil
}

func civilToJSON(d *model.CivilDate) *civilDateJSON {
	if d == nil {
		return nil
	}
	return &civilDateJSON{Year: d.Year, Month: d.Month, Day: d.Day}
}

func civilFromJSON(d *civilDateJSON) *model.CivilDate {
	if d == nil {
		return nil
	}
	return &model.CivilDate{Year: d.Year, Month: d.Month, Day: d.Day}
}

// SnapshotEqual reports whether two snapshots carry the same inputs.
// Times compare by instant (time.Equal), so a JSON round-trip that loses
// the location name still matches; nil pointers must match nil.
func SnapshotEqual(a, b model.Snapshot) bool {
	if a.Text != b.Text || a.Marker != b.Marker || a.Duration != b.Duration || a.Priority != b.Priority || a.Order != b.Order {
		return false
	}
	// Nil and empty slices both mean "none" (len 0); element order matters
	// otherwise.
	if !slices.Equal(a.TimeMaps, b.TimeMaps) {
		return false
	}
	if !civilEqual(a.Deadline, b.Deadline) || !civilEqual(a.PlanStart, b.PlanStart) || !civilEqual(a.PlanEnd, b.PlanEnd) {
		return false
	}
	if !slices.Equal(a.Depends, b.Depends) {
		return false
	}
	if a.Pin == nil || b.Pin == nil {
		return a.Pin == nil && b.Pin == nil
	}
	return a.Pin.Equal(*b.Pin)
}

func civilEqual(a, b *model.CivilDate) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
