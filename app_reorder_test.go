package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Mordeak/cp77-modorder-gui/internal/config"
	"github.com/Mordeak/cp77-modorder-gui/internal/conflict"
)

func TestReorderConflictGroupAddsNewModToModlist(t *testing.T) {
	a, mods := newReorderTestApp(t,
		[]string{"existing.archive", "middle.archive"},
		[]string{"existing.archive", "middle.archive", "new.archive"},
		"existing.archive", "new.archive",
	)

	_, err := a.ReorderConflictGroup(
		[]string{"new.archive", "existing.archive"},
		"new.archive",
	)
	if err != nil {
		t.Fatalf("ReorderConflictGroup() error = %v", err)
	}

	want := []string{"new.archive", "existing.archive", "middle.archive"}
	if !slices.Equal(a.modlistOrder, want) {
		t.Fatalf("modlist order = %v, want %v", a.modlistOrder, want)
	}
	if mods["new.archive"].Wins != 1 || mods["existing.archive"].Losses != 1 {
		t.Fatalf("conflict result not recomputed: new wins=%d, existing losses=%d",
			mods["new.archive"].Wins, mods["existing.archive"].Losses)
	}

	if err := a.WriteModlist(); err != nil {
		t.Fatalf("WriteModlist() error = %v", err)
	}
	data, err := os.ReadFile(filepath.Join(a.modDir, "modlist.txt"))
	if err != nil {
		t.Fatalf("read modlist.txt: %v", err)
	}
	gotFileOrder := strings.Fields(string(data))
	if !slices.Equal(gotFileOrder, want) {
		t.Fatalf("written modlist order = %v, want %v", gotFileOrder, want)
	}
}

func TestReorderConflictGroupMovesOnlyDraggedMod(t *testing.T) {
	a, _ := newReorderTestApp(t,
		[]string{"winner.archive", "middle-one.archive", "middle-two.archive", "loser.archive"},
		[]string{"winner.archive", "middle-one.archive", "middle-two.archive", "loser.archive"},
		"winner.archive", "loser.archive",
	)

	_, err := a.ReorderConflictGroup(
		[]string{"loser.archive", "winner.archive"},
		"loser.archive",
	)
	if err != nil {
		t.Fatalf("ReorderConflictGroup() error = %v", err)
	}

	want := []string{"loser.archive", "winner.archive", "middle-one.archive", "middle-two.archive"}
	if !slices.Equal(a.modlistOrder, want) {
		t.Fatalf("modlist order = %v, want insertion order %v", a.modlistOrder, want)
	}
}

func TestReorderConflictGroupNoOpPreservesSpacing(t *testing.T) {
	a, _ := newReorderTestApp(t,
		[]string{"winner.archive", "middle.archive", "loser.archive"},
		[]string{"winner.archive", "middle.archive", "loser.archive"},
		"winner.archive", "loser.archive",
	)

	_, err := a.ReorderConflictGroup(
		[]string{"winner.archive", "loser.archive"},
		"winner.archive",
	)
	if err != nil {
		t.Fatalf("ReorderConflictGroup() error = %v", err)
	}

	want := []string{"winner.archive", "middle.archive", "loser.archive"}
	if !slices.Equal(a.modlistOrder, want) {
		t.Fatalf("no-op order = %v, want %v", a.modlistOrder, want)
	}
}

func TestReconcileSavedOrderRestoresUnappliedNewMod(t *testing.T) {
	diskOrder := []string{"before.archive", "z-core.archive", "after.archive"}
	a, mods := newReorderTestApp(t,
		diskOrder,
		[]string{"before.archive", "z-core.archive", "after.archive", "new.archive"},
		"z-core.archive", "new.archive",
	)

	savedPriorities := map[string]int{
		"before.archive": 1,
		"new.archive":    2,
		"z-core.archive": 3,
		"after.archive":  4,
	}
	for name, priority := range savedPriorities {
		mods[name].Priority = priority
	}
	a.cfg.Priorities = savedPriorities
	a.result.ApplyPriorities()

	order, diskSet := a.reconcileSavedOrder(diskOrder)
	want := []string{"before.archive", "new.archive", "z-core.archive", "after.archive"}
	if !slices.Equal(order, want) {
		t.Fatalf("reconciled order = %v, want %v", order, want)
	}
	if diskSet["new.archive"] {
		t.Fatal("new archive should remain absent from the on-disk membership set")
	}

	a.modlistOrder = order
	a.modlistSet = diskSet
	result := a.buildScanResult()
	if result.Rows[1].Name != "new.archive" || !result.Rows[1].Unlisted {
		t.Fatalf("restored row = %+v, want new archive marked unlisted at its saved position", result.Rows[1])
	}

	if err := a.WriteModlist(); err != nil {
		t.Fatalf("WriteModlist() error = %v", err)
	}
	data, err := os.ReadFile(filepath.Join(a.modDir, "modlist.txt"))
	if err != nil {
		t.Fatalf("read modlist.txt: %v", err)
	}
	if got := strings.Fields(string(data)); !slices.Equal(got, want) {
		t.Fatalf("written restored order = %v, want %v", got, want)
	}
}

func TestReconcileSavedOrderLeavesUnprioritisedNewModAtBottom(t *testing.T) {
	diskOrder := []string{"first.archive", "second.archive"}
	a, _ := newReorderTestApp(t,
		diskOrder,
		[]string{"first.archive", "second.archive", "new.archive"},
		"second.archive", "new.archive",
	)

	order, diskSet := a.reconcileSavedOrder(diskOrder)
	if !slices.Equal(order, diskOrder) {
		t.Fatalf("reconciled order = %v, want unchanged disk order %v", order, diskOrder)
	}
	if diskSet["new.archive"] {
		t.Fatal("unprioritised new archive should not be in the on-disk membership set")
	}
}

func newReorderTestApp(
	t *testing.T,
	modlistOrder []string,
	resultOrder []string,
	conflictingNames ...string,
) (*App, map[string]*conflict.ModInfo) {
	t.Helper()

	modsByName := make(map[string]*conflict.ModInfo, len(resultOrder))
	mods := make([]*conflict.ModInfo, 0, len(resultOrder))
	priorities := make(map[string]int, len(modlistOrder))
	for i, name := range modlistOrder {
		priorities[name] = i + 1
	}
	for _, name := range resultOrder {
		m := &conflict.ModInfo{Name: name, Priority: priorities[name]}
		modsByName[name] = m
		mods = append(mods, m)
	}

	contestants := make([]*conflict.ModInfo, 0, len(conflictingNames))
	for _, name := range conflictingNames {
		contestants = append(contestants, modsByName[name])
	}
	result := &conflict.Result{
		Mods: mods,
		Conflicts: []*conflict.ConflictEntry{
			{Resource: "0x1", Mods: contestants},
		},
	}
	result.ApplyPriorities()

	modlistSet := make(map[string]bool, len(modlistOrder))
	for _, name := range modlistOrder {
		modlistSet[name] = true
	}
	tempDir := t.TempDir()

	return &App{
		cfg:          &config.Config{Priorities: priorities},
		cfgPath:      filepath.Join(tempDir, "config", "config.json"),
		result:       result,
		modDir:       tempDir,
		modlistOrder: append([]string(nil), modlistOrder...),
		modlistSet:   modlistSet,
	}, modsByName
}
