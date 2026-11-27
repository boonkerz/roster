package api

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/boonkerz/roster/internal/server/auth"
	"github.com/boonkerz/roster/internal/server/config"
	"github.com/boonkerz/roster/internal/server/model"
	"github.com/boonkerz/roster/internal/server/store"
	"github.com/boonkerz/roster/internal/shared"
)

// TestStagedFlowRollout: höchstens N Geräte gleichzeitig, Nachrücken nach Ergebnis, Halt bei
// Fehler mit Fortsetzen, Abbrechen der Wartenden und Platzfreigabe nach Zeitablauf.
func TestStagedFlowRollout(t *testing.T) {
	st, err := store.Open("sqlite://" + filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	s := &Server{store: st, cfg: config.Config{}, log: slog.New(slog.NewTextHandler(io.Discard, nil)), term: newTermHub()}
	var ids []string
	for _, name := range []string{"a", "b", "c", "d", "e"} {
		d := &model.Device{ID: store.NewID(), Hostname: name, OS: "ubuntu"}
		if err := st.CreateDevice(ctx, d, auth.HashToken("t"+name)); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, d.ID)
	}
	g := shared.FlowGraph{Nodes: []shared.FlowNode{{ID: "s", Type: "start"}}}
	run := &model.FlowRun{FlowID: "f", FlowName: "Test", Graph: g, Params: map[string]string{}, TestDeviceID: "x"}
	if err := st.CreateFlowRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	if err := st.SetFlowRunRollout(ctx, run.ID, 2, true); err != nil {
		t.Fatal(err)
	}
	if err := st.AddPendingFlowRunDevices(ctx, run.ID, ids); err != nil {
		t.Fatal(err)
	}
	count := func(status string) (n int) {
		r, _ := st.GetFlowRun(ctx, run.ID)
		for _, d := range r.Devices {
			if d.Status == status {
				n++
			}
		}
		return
	}
	cmdOf := func(host string) string {
		r, _ := st.GetFlowRun(ctx, run.ID)
		for _, d := range r.Devices {
			if d.Hostname == host {
				return d.CommandID
			}
		}
		return ""
	}
	if n, err := s.dispatchFlowRun(ctx, run.ID); err != nil || n != 2 {
		t.Fatalf("erster verteiler: %d %v", n, err)
	}
	if count("queued") != 2 || count("pending") != 3 {
		t.Fatalf("queued=%d pending=%d", count("queued"), count("pending"))
	}
	// Zweiter Lauf ohne Ergebnis: nichts Neues.
	if n, _ := s.dispatchFlowRun(ctx, run.ID); n != 0 {
		t.Fatalf("ohne freien platz eingereiht: %d", n)
	}
	// a meldet ok → c rückt nach (alphabetisch).
	s.completeFlowRuns(ctx, []shared.CommandResult{{CommandID: cmdOf("a"), Output: `{"status":"ok"}`}})
	if count("queued") != 2 || count("pending") != 2 || cmdOf("c") == "" {
		t.Fatalf("nachrücken: queued=%d pending=%d c=%q", count("queued"), count("pending"), cmdOf("c"))
	}
	// b scheitert → Halt: trotz freiem Platz kein Start.
	s.completeFlowRuns(ctx, []shared.CommandResult{{CommandID: cmdOf("b"), Output: `{"status":"fail","error":"pull"}`}})
	r, _ := st.GetFlowRun(ctx, run.ID)
	if !r.Halted || count("pending") != 2 {
		t.Fatalf("kein halt: halted=%v pending=%d", r.Halted, count("pending"))
	}
	// Fortsetzen → d startet.
	if err := st.ResumeFlowRun(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.dispatchFlowRun(ctx, run.ID); n != 1 || cmdOf("d") == "" {
		t.Fatalf("fortsetzen: %d", n)
	}
	// c hängt (offline): Platz nach Zeitablauf frei → e startet.
	old := time.Now().Add(-flowSlotTimeout - time.Minute)
	if _, err := st.DB().ExecContext(ctx, "UPDATE flow_run_devices SET updated_at=? WHERE command_id=?", old, cmdOf("c")); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.dispatchFlowRun(ctx, run.ID); n != 1 || cmdOf("e") == "" {
		t.Fatalf("zeitablauf: %d", n)
	}
	if count("pending") != 0 {
		t.Fatalf("pending=%d", count("pending"))
	}
	// Abbrechen: neuer Lauf mit Wartenden.
	run2 := &model.FlowRun{FlowID: "f", FlowName: "Test", Graph: g, Params: map[string]string{}}
	if err := st.CreateFlowRun(ctx, run2); err != nil {
		t.Fatal(err)
	}
	if err := st.SetFlowRunRollout(ctx, run2.ID, 1, false); err != nil {
		t.Fatal(err)
	}
	if err := st.AddPendingFlowRunDevices(ctx, run2.ID, ids); err != nil {
		t.Fatal(err)
	}
	if _, err := s.dispatchFlowRun(ctx, run2.ID); err != nil {
		t.Fatal(err)
	}
	n, err := st.CancelFlowRun(ctx, run2.ID)
	if err != nil || n != 4 {
		t.Fatalf("abbrechen: %d %v", n, err)
	}
	runs, _ := st.FlowRunsWithPending(ctx)
	if len(runs) != 0 {
		t.Fatalf("noch wartende läufe: %v", runs)
	}
	sums, _ := st.ListFlowRuns(ctx, "f", 10)
	if len(sums) != 2 {
		t.Fatalf("läufe: %d", len(sums))
	}
}
