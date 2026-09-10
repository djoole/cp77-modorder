package main

import (
	"slices"
	"testing"

	"github.com/Mordeak/cp77-modorder-gui/internal/conflict"
)

func TestInsertNewArchivesASCIIAtNaturalPositions(t *testing.T) {
	mods := []*conflict.ModInfo{
		{Name: "axellysse_alpha.archive"},
		{Name: "axellysse_atelier_store.archive"},
		{Name: "axellysse_clothes.archive"},
		{Name: "axellysse_pearls_swimsuit.archive"},
		{Name: "axellysse_virtual_atelier_2.archive"},
		{Name: "axellysse_zeta.archive"},
	}
	existing := []string{
		"axellysse_alpha.archive",
		"axellysse_clothes.archive",
		"axellysse_zeta.archive",
	}

	got := insertNewArchivesASCII(existing, mods)
	want := []string{
		"axellysse_alpha.archive",
		"axellysse_atelier_store.archive",
		"axellysse_clothes.archive",
		"axellysse_pearls_swimsuit.archive",
		"axellysse_virtual_atelier_2.archive",
		"axellysse_zeta.archive",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("completed order = %v, want %v", got, want)
	}
}

func TestInsertNewArchivesASCIIPreservesExistingManualOrder(t *testing.T) {
	mods := []*conflict.ModInfo{
		{Name: "alpha.archive"},
		{Name: "bravo.archive"},
		{Name: "charlie.archive"},
		{Name: "delta.archive"},
		{Name: "echo.archive"},
	}
	existing := []string{"charlie.archive", "alpha.archive", "echo.archive"}

	got := insertNewArchivesASCII(existing, mods)
	want := []string{"charlie.archive", "alpha.archive", "bravo.archive", "delta.archive", "echo.archive"}
	if !slices.Equal(got, want) {
		t.Fatalf("completed order = %v, want %v", got, want)
	}

	retained := make([]string, 0, len(existing))
	for _, name := range got {
		if slices.Contains(existing, name) {
			retained = append(retained, name)
		}
	}
	if !slices.Equal(retained, existing) {
		t.Fatalf("existing order changed from %v to %v", existing, retained)
	}
}

func TestInsertNewArchivesASCIIIsNotShiftedByLowerNamedArchiveAtBottom(t *testing.T) {
	tests := []struct {
		name     string
		archive  string
		existing []string
		want     []string
	}{
		{
			name:    "HardestToBeGrowlFM",
			archive: "HardestToBeGrowlFM.archive",
			existing: []string{
				"Hailey_Task_Force_Outfit_XL.archive",
				"HeresBrendan.archive",
				"HighResNPCBodies.archive",
				"HD Reworked Project.archive",
			},
			want: []string{
				"Hailey_Task_Force_Outfit_XL.archive",
				"HardestToBeGrowlFM.archive",
				"HeresBrendan.archive",
				"HighResNPCBodies.archive",
				"HD Reworked Project.archive",
			},
		},
		{
			name:    "ITP-TCOC2",
			archive: "ITP-TCOC2.archive",
			existing: []string{
				"ITP-TCOC.archive",
				"Iconic Cyberarm Pack.archive",
				"IdleAnywhere.archive",
				"HD Reworked Project.archive",
			},
			want: []string{
				"ITP-TCOC.archive",
				"ITP-TCOC2.archive",
				"Iconic Cyberarm Pack.archive",
				"IdleAnywhere.archive",
				"HD Reworked Project.archive",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mods := make([]*conflict.ModInfo, 0, len(tt.existing)+1)
			for _, name := range tt.existing {
				mods = append(mods, &conflict.ModInfo{Name: name})
			}
			mods = append(mods, &conflict.ModInfo{Name: tt.archive})

			got := insertNewArchivesASCII(tt.existing, mods)
			if !slices.Equal(got, tt.want) {
				t.Fatalf("completed order = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestInsertNewArchivesASCIIHandlesMultipleLowerNamedOutliers(t *testing.T) {
	existing := []string{
		"Hailey.archive",
		"Heres.archive",
		"High.archive",
		"A-manually-moved.archive",
		"B-manually-moved.archive",
	}
	mods := []*conflict.ModInfo{
		{Name: "Hailey.archive"},
		{Name: "Hardest.archive"},
		{Name: "Heres.archive"},
		{Name: "High.archive"},
		{Name: "A-manually-moved.archive"},
		{Name: "B-manually-moved.archive"},
	}

	got := insertNewArchivesASCII(existing, mods)
	want := []string{
		"Hailey.archive",
		"Hardest.archive",
		"Heres.archive",
		"High.archive",
		"A-manually-moved.archive",
		"B-manually-moved.archive",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("completed order = %v, want %v", got, want)
	}
}

func TestBuildScanResultShowsNewArchiveOnceAtProposedPosition(t *testing.T) {
	mods := []*conflict.ModInfo{
		{Name: "alpha.archive"},
		{Name: "bravo.archive"},
		{Name: "charlie.archive"},
	}
	a := &App{
		result:       &conflict.Result{Mods: mods},
		modlistOrder: []string{"alpha.archive", "bravo.archive", "charlie.archive"},
		modlistSet:   map[string]bool{"alpha.archive": true, "charlie.archive": true},
	}

	result := a.buildScanResult()
	if len(result.Rows) != 3 {
		t.Fatalf("row count = %d, want 3 without duplicate new archive", len(result.Rows))
	}
	if result.Rows[1].Name != "bravo.archive" || !result.Rows[1].Unlisted {
		t.Fatalf("middle row = %+v, want bravo.archive marked new", result.Rows[1])
	}
	ordered := a.modsInDisplayOrder()
	if len(ordered) != 3 || ordered[1].Name != "bravo.archive" {
		t.Fatalf("display order = %v, want new archive once in the middle", ordered)
	}
}

func TestRescanInsertsFreshArchivesAfterRestoringEarlierUnappliedNewMod(t *testing.T) {
	diskOrder := []string{
		"axellysse_alpha.archive",
		"axellysse_clothes.archive",
		"axellysse_zeta.archive",
	}
	a, mods := newReorderTestApp(t,
		diskOrder,
		[]string{
			"axellysse_alpha.archive",
			"axellysse_atelier_store.archive",
			"axellysse_clothes.archive",
			"axellysse_zeta.archive",
			"axellysse_pearls_swimsuit.archive",
			"axellysse_virtual_atelier_2.archive",
		},
		"axellysse_alpha.archive", "axellysse_zeta.archive",
	)

	// atelier_store was inserted during the previous scan but not applied.
	// The other two archives are being discovered for the first time now.
	savedPriorities := map[string]int{
		"axellysse_alpha.archive":         1,
		"axellysse_atelier_store.archive": 2,
		"axellysse_clothes.archive":       3,
		"axellysse_zeta.archive":          4,
	}
	for name, priority := range savedPriorities {
		mods[name].Priority = priority
	}
	a.cfg.Priorities = savedPriorities
	a.result.ApplyPriorities()

	order, diskSet := a.reconcileSavedOrder(diskOrder)
	order = insertNewArchivesASCII(order, a.result.Mods)
	want := []string{
		"axellysse_alpha.archive",
		"axellysse_atelier_store.archive",
		"axellysse_clothes.archive",
		"axellysse_pearls_swimsuit.archive",
		"axellysse_virtual_atelier_2.archive",
		"axellysse_zeta.archive",
	}
	if !slices.Equal(order, want) {
		t.Fatalf("rescanned order = %v, want %v", order, want)
	}
	for _, name := range []string{
		"axellysse_atelier_store.archive",
		"axellysse_pearls_swimsuit.archive",
		"axellysse_virtual_atelier_2.archive",
	} {
		if diskSet[name] {
			t.Fatalf("%s should remain marked as new before Apply", name)
		}
	}
}
