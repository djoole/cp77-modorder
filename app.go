package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/Mordeak/cp77-modorder-gui/internal/archive"
	"github.com/Mordeak/cp77-modorder-gui/internal/config"
	"github.com/Mordeak/cp77-modorder-gui/internal/conflict"
	"github.com/Mordeak/cp77-modorder-gui/internal/modlist"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App is the Wails application struct. All exported methods are auto-bound to JS.
type App struct {
	ctx                 context.Context
	cfg                 *config.Config
	cfgPath             string
	result              *conflict.Result
	modDir              string
	modlistOrder        []string
	modlistSet          map[string]bool
	initialModlistOrder []string          // snapshot of modlist order at last scan, used for Apply diff
	pathMap             map[string]string // "0x<hex>" → human-readable resource path (from LXRS footers)
	modStructure        string            // "default" | "MO2" — set from CLI flag, not persisted
}

// NewApp creates the App with a loaded config.
// modStructure should be "default" or "MO2" (from the --mod-structure flag).
func NewApp(modStructure string) *App {
	cfgPath := config.DefaultPath()
	cfg, err := config.Load(cfgPath)
	if err != nil {
		cfg = &config.Config{Priorities: make(map[string]int)}
	}
	if modStructure == "" {
		modStructure = "default"
	}
	return &App{
		cfg:          cfg,
		cfgPath:      cfgPath,
		modDir:       cfg.ModDir,
		modStructure: modStructure,
	}
}

// startup is called when Wails is ready; saves the context for runtime calls.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

// ---- Bound methods --------------------------------------------------------

// GetConfig returns persisted config for use on startup.
func (a *App) GetConfig() ConfigDTO {
	return ConfigDTO{
		ModDir:       a.cfg.ModDir,
		ModStructure: a.modStructure,
		MO2Dir:       a.cfg.MO2Dir,
		MO2Profile:   a.cfg.MO2Profile,
		BackupLimit:  a.cfg.BackupLimit,
	}
}

// GetMO2Profiles lists the profile names available in an MO2 instance directory.
func (a *App) GetMO2Profiles(instanceDir string) ([]string, error) {
	profilesDir := filepath.Join(strings.TrimSpace(instanceDir), "profiles")
	entries, err := os.ReadDir(profilesDir)
	if err != nil {
		return nil, fmt.Errorf("read MO2 profiles dir: %w", err)
	}

	var profiles []string
	for _, e := range entries {
		if e.IsDir() {
			profiles = append(profiles, e.Name())
		}
	}

	return profiles, nil
}

// ScanMO2 reads the enabled mods from an MO2 profile, parses their archives, and
// returns the full conflict result. The instance dir and profile are saved to config.
func (a *App) ScanMO2(instanceDir, profile string) (ScanResultDTO, error) {
	instanceDir = strings.TrimSpace(instanceDir)
	profile = strings.TrimSpace(profile)
	if instanceDir == "" {
		return ScanResultDTO{}, fmt.Errorf("no MO2 instance path provided")
	}
	if profile == "" {
		return ScanResultDTO{}, fmt.Errorf("no profile selected")
	}

	profileModlist := filepath.Join(instanceDir, "profiles", profile, "modlist.txt")
	data, err := os.ReadFile(profileModlist)
	if err != nil {
		return ScanResultDTO{}, fmt.Errorf("read MO2 profile modlist: %w", err)
	}

	// Collect enabled mod names in file order (MO2: bottom of list = highest priority).
	var enabledFileOrder []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "+") {
			enabledFileOrder = append(enabledFileOrder, line[1:])
		}
	}
	if len(enabledFileOrder) == 0 {
		return ScanResultDTO{}, fmt.Errorf("no enabled mods found in MO2 profile %q", profile)
	}

	// Reverse: MO2 bottom = highest priority → first in CP77 load order (first wins).
	enabledMods := make([]string, len(enabledFileOrder))
	for i, name := range enabledFileOrder {
		enabledMods[len(enabledFileOrder)-1-i] = name
	}

	runtime.EventsEmit(a.ctx, "scan:progress", fmt.Sprintf("Scanning MO2 profile %q — %d enabled mods…", profile, len(enabledMods)))

	modsDir := filepath.Join(instanceDir, "mods")
	archives, err := archive.ScanMO2(modsDir, enabledMods)
	if err != nil {
		return ScanResultDTO{}, err
	}
	if len(archives) == 0 {
		return ScanResultDTO{}, fmt.Errorf("no .archive files found in enabled MO2 mods")
	}

	a.result = conflict.Detect(archives, a.cfg.Priorities)

	pathMap := make(map[string]string)
	for _, ar := range archives {
		for hash, path := range ar.FilePaths {
			pathMap[fmt.Sprintf("0x%016x", hash)] = path
		}
	}
	a.pathMap = pathMap

	// Use the conflict-sorted order as the initial modlist order.
	a.modlistOrder = make([]string, len(a.result.Mods))
	for i, m := range a.result.Mods {
		a.modlistOrder[i] = m.Name
	}
	a.modlistSet = make(map[string]bool, len(a.modlistOrder))
	for _, n := range a.modlistOrder {
		a.modlistSet[n] = true
	}

	snap := make([]string, len(a.modlistOrder))
	copy(snap, a.modlistOrder)
	a.initialModlistOrder = snap

	a.modDir = modsDir
	a.cfg.MO2Dir = instanceDir
	a.cfg.MO2Profile = profile
	_ = a.cfg.Save(a.cfgPath)

	runtime.EventsEmit(a.ctx, "scan:progress", a.result.Summary())

	return a.buildScanResult(), nil
}

// PickFolder opens the native OS folder picker and returns the chosen path.
func (a *App) PickFolder() string {
	dir, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Select Cyberpunk 2077 mods folder",
	})
	if err != nil {
		return ""
	}

	return dir
}

// Scan scans dir for .archive files, detects conflicts, and returns the full result.
// It also saves modDir to config. Progress events are emitted as "scan:progress".
func (a *App) Scan(dir string) (ScanResultDTO, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return ScanResultDTO{}, fmt.Errorf("no folder path provided")
	}

	runtime.EventsEmit(a.ctx, "scan:progress", "Scanning "+dir+" …")

	archives, err := archive.Scan(dir)
	if err != nil {
		return ScanResultDTO{}, err
	}
	if len(archives) == 0 {
		// Count raw .archive files to give a better error message.
		var rawCount int
		if entries, readErr := os.ReadDir(dir); readErr == nil {
			for _, e := range entries {
				if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ".archive") {
					rawCount++
				}
			}
		}
		if rawCount > 0 {
			return ScanResultDTO{}, fmt.Errorf(
				"found %d .archive file(s) in %s but none could be read as valid RED4 archives",
				rawCount, dir,
			)
		}
		return ScanResultDTO{}, fmt.Errorf("no .archive files found in %s", dir)
	}

	// Read modlist order before detection so archives can be pre-sorted to match it.
	// conflict.Detect's sortMods preserves input order for unset-priority mods, so
	// the pre-sort here is what drives win/loss computation when no explicit priority is set.
	order, _ := a.readModlistOrder(dir)
	if len(order) > 0 {
		posMap := make(map[string]int, len(order))
		for i, n := range order {
			posMap[n] = i
		}
		sort.SliceStable(archives, func(i, j int) bool {
			pi, iIn := posMap[archives[i].Name]
			pj, jIn := posMap[archives[j].Name]
			if iIn && jIn {
				return pi < pj
			}
			if iIn {
				return true
			}
			if jIn {
				return false
			}
			return archives[i].Name < archives[j].Name
		})
	}

	a.result = conflict.Detect(archives, a.cfg.Priorities)

	// Build a merged hash→path map from all archives that have LXRS data.
	pathMap := make(map[string]string)
	for _, ar := range archives {
		for hash, path := range ar.FilePaths {
			pathMap[fmt.Sprintf("0x%016x", hash)] = path
		}
	}
	a.pathMap = pathMap

	if len(order) == 0 {
		// No modlist.txt yet — create one with archives sorted alphabetically.
		alpha := make([]*conflict.ModInfo, len(a.result.Mods))
		copy(alpha, a.result.Mods)
		sort.Slice(alpha, func(i, j int) bool { return alpha[i].Name < alpha[j].Name })
		if err := modlist.Write(dir, alpha, ""); err == nil {
			order = make([]string, len(alpha))
			for i, m := range alpha {
				order[i] = m.Name
			}
		}
	}
	a.modlistOrder = order
	a.modlistSet = make(map[string]bool, len(order))
	for _, n := range order {
		a.modlistSet[n] = true
	}

	snap := make([]string, len(order))
	copy(snap, order)
	a.initialModlistOrder = snap

	// Persist the directory.
	a.modDir = dir
	a.cfg.ModDir = dir
	_ = a.cfg.Save(a.cfgPath)

	runtime.EventsEmit(a.ctx, "scan:progress", a.result.Summary())

	return a.buildScanResult(), nil
}

// SetPriority sets the priority for mod name (0 = clear) and returns the updated result.
func (a *App) SetPriority(name string, p int) (ScanResultDTO, error) {
	if a.result == nil {
		return ScanResultDTO{}, fmt.Errorf("no scan results — run a scan first")
	}
	if p < 0 || p > 99 {
		return ScanResultDTO{}, fmt.Errorf("priority must be 0–99")
	}
	for _, m := range a.result.Mods {
		if m.Name == name {
			m.Priority = p
			if p == 0 {
				delete(a.cfg.Priorities, name)
			} else {
				a.cfg.Priorities[name] = p
			}
			break
		}
	}
	_ = a.cfg.Save(a.cfgPath)
	a.result.ApplyPriorities()

	return a.buildScanResult(), nil
}

// GetConflictGroup returns the ordered list of mods that share at least one
// conflicting resource with the named mod.
func (a *App) GetConflictGroup(name string) (ConflictGroupDTO, error) {
	if a.result == nil {
		return ConflictGroupDTO{}, fmt.Errorf("no scan results")
	}
	var anchor *conflict.ModInfo
	for _, m := range a.result.Mods {
		if m.Name == name {
			anchor = m
			break
		}
	}
	if anchor == nil {
		return ConflictGroupDTO{}, fmt.Errorf("mod %q not found", name)
	}
	group := a.conflictGroup(anchor)
	names := make([]string, len(group))
	for i, m := range group {
		names[i] = m.Name
	}

	return ConflictGroupDTO{Mods: names}, nil
}

// ReorderConflictGroup moves one dragged mod within its conflict group while
// preserving the global positions of every untouched mod. The requested group
// order is used to determine whether the dragged mod should be inserted before
// its next group neighbour or after its previous one.
func (a *App) ReorderConflictGroup(names []string, movedName string) (ScanResultDTO, error) {
	if a.result == nil {
		return ScanResultDTO{}, fmt.Errorf("no scan results")
	}

	if len(names) < 2 {
		return a.buildScanResult(), nil
	}

	movedIdx := slices.Index(names, movedName)
	if movedIdx < 0 {
		return ScanResultDTO{}, fmt.Errorf("moved mod %q is not in the conflict group", movedName)
	}

	order := a.completeModlistOrder()
	currentGroupOrder := make([]string, 0, len(names))
	groupSet := make(map[string]bool, len(names))
	for _, name := range names {
		if groupSet[name] {
			return ScanResultDTO{}, fmt.Errorf("duplicate mod %q in conflict group", name)
		}
		groupSet[name] = true
	}
	for _, name := range order {
		if groupSet[name] {
			currentGroupOrder = append(currentGroupOrder, name)
		}
	}
	if len(currentGroupOrder) != len(names) {
		return ScanResultDTO{}, fmt.Errorf("conflict group contains an unknown mod")
	}
	if slices.Equal(currentGroupOrder, names) {
		return a.buildScanResult(), nil
	}

	// Remove only the dragged mod; every untouched row keeps its global place.
	oldIdx := slices.Index(order, movedName)
	if oldIdx < 0 {
		return ScanResultDTO{}, fmt.Errorf("moved mod %q is not in the load order", movedName)
	}
	order = append(order[:oldIdx], order[oldIdx+1:]...)

	insertAt := -1
	if movedIdx+1 < len(names) {
		// Moving earlier: insert immediately before the next conflict neighbour.
		insertAt = slices.Index(order, names[movedIdx+1])
	} else {
		// Moving to the end: insert immediately after the previous neighbour.
		prevIdx := slices.Index(order, names[movedIdx-1])
		if prevIdx >= 0 {
			insertAt = prevIdx + 1
		}
	}
	if insertAt < 0 {
		return ScanResultDTO{}, fmt.Errorf("could not locate insertion point for %q", movedName)
	}

	order = slices.Insert(order, insertAt, movedName)
	a.setModlistOrder(order)

	return a.buildScanResult(), nil
}

// SetModlistOrder replaces the in-memory load order with the provided list and
// recomputes wins/losses to match. The change is not written to disk until the
// user clicks Apply (WriteModlist).
func (a *App) SetModlistOrder(names []string) (ScanResultDTO, error) {
	if a.result == nil {
		return ScanResultDTO{}, fmt.Errorf("no scan results")
	}
	a.setModlistOrder(names)

	return a.buildScanResult(), nil
}

// GetApplyPreview returns the ordered mod names that will be written to modlist.txt,
// plus the order at last scan so the frontend can show a diff.
func (a *App) GetApplyPreview() (ApplyPreviewDTO, error) {
	if a.result == nil {
		return ApplyPreviewDTO{}, fmt.Errorf("no scan results — run a scan first")
	}
	mods := a.modsInDisplayOrder()
	names := make([]string, len(mods))
	for i, m := range mods {
		names[i] = m.Name
	}

	current := make([]string, len(a.initialModlistOrder))
	copy(current, a.initialModlistOrder)

	return ApplyPreviewDTO{Names: names, Current: current}, nil
}

// ListBackups returns backup filenames from modlist.old/, newest first.
func (a *App) ListBackups() ([]string, error) {
	var backupDir string
	if a.modStructure == "MO2" && a.cfg.MO2Dir != "" && a.cfg.MO2Profile != "" {
		backupDir = filepath.Join(a.cfg.MO2Dir, "profiles", a.cfg.MO2Profile, "modlist.old")
	} else if a.modDir != "" {
		backupDir = filepath.Join(a.modDir, "modlist.old")
	} else {
		return []string{}, nil
	}
	entries, err := os.ReadDir(backupDir)
	if os.IsNotExist(err) {
		return []string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read backup dir: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	// Newest first (timestamps sort lexicographically, so reverse).
	sort.Slice(names, func(i, j int) bool { return names[i] > names[j] })

	return names, nil
}

// RestoreBackup copies a file from modlist.old/ to modlist.txt and reloads the order.
func (a *App) RestoreBackup(filename string) (ScanResultDTO, error) {
	if a.modDir == "" {
		return ScanResultDTO{}, fmt.Errorf("no mod directory set")
	}
	if strings.ContainsAny(filename, "/\\") {
		return ScanResultDTO{}, fmt.Errorf("invalid backup filename")
	}
	data, err := os.ReadFile(filepath.Join(a.modDir, "modlist.old", filename))
	if err != nil {
		return ScanResultDTO{}, fmt.Errorf("read backup: %w", err)
	}

	if err := os.WriteFile(filepath.Join(a.modDir, "modlist.txt"), data, 0o644); err != nil {
		return ScanResultDTO{}, fmt.Errorf("write modlist.txt: %w", err)
	}

	order, err := a.readModlistOrder(a.modDir)
	if err != nil {
		return ScanResultDTO{}, fmt.Errorf("reload modlist: %w", err)
	}

	a.modlistOrder = order
	a.modlistSet = make(map[string]bool, len(order))
	for _, n := range order {
		a.modlistSet[n] = true
	}

	return a.buildScanResult(), nil
}

// WriteModlist writes the mod order to disk.
// In MO2 mode it rewrites the MO2 profile modlist.txt, reordering enabled entries
// and preserving disabled mods and separators. In default mode it writes modlist.txt.
func (a *App) WriteModlist() error {
	if a.result == nil {
		return fmt.Errorf("no scan results — run a scan first")
	}
	if a.modStructure == "MO2" {
		if a.cfg.MO2Dir == "" || a.cfg.MO2Profile == "" {
			return fmt.Errorf("MO2 instance or profile not set — run a scan first")
		}
		return a.writeMO2Modlist()
	}
	if a.modDir == "" {
		return fmt.Errorf("no mod directory set")
	}
	if err := modlist.Write(a.modDir, a.modsInDisplayOrder(), ""); err != nil {
		return err
	}
	_ = modlist.PruneBackups(filepath.Join(a.modDir, "modlist.old"), a.cfg.EffectiveBackupLimit())

	return nil
}

// writeMO2Modlist rewrites the MO2 profile modlist.txt, preserving disabled mods
// and separators but replacing the enabled entries with the current result order
// (reversed, since MO2 is bottom = highest priority).
func (a *App) writeMO2Modlist() error {
	profileDir := filepath.Join(a.cfg.MO2Dir, "profiles", a.cfg.MO2Profile)
	dest := filepath.Join(profileDir, "modlist.txt")

	data, err := os.ReadFile(dest)
	if err != nil {
		return fmt.Errorf("read MO2 profile modlist: %w", err)
	}

	// Backup into modlist.old/ inside the profile dir.
	backupDir := filepath.Join(profileDir, "modlist.old")
	if mkErr := os.MkdirAll(backupDir, 0o755); mkErr != nil {
		return fmt.Errorf("create backup dir: %w", mkErr)
	}
	ts := time.Now().Format("2006-01-02_15-04-05")
	if wErr := os.WriteFile(filepath.Join(backupDir, "modlist.txt."+ts), data, 0o644); wErr != nil {
		return fmt.Errorf("backup MO2 modlist: %w", wErr)
	}
	_ = modlist.PruneBackups(backupDir, a.cfg.EffectiveBackupLimit())

	// Parse lines, record the positions of enabled entries.
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	var enabledIdx []int
	for i, line := range lines {
		if strings.HasPrefix(line, "+") {
			enabledIdx = append(enabledIdx, i)
		}
	}

	// Build new enabled order: current display order reversed (MO2 bottom = highest priority).
	ordered := a.modsInDisplayOrder()
	reversedNames := make([]string, len(ordered))
	for i, m := range ordered {
		reversedNames[len(ordered)-1-i] = m.Name
	}

	count := len(enabledIdx)
	if len(reversedNames) < count {
		count = len(reversedNames)
	}
	for i := 0; i < count; i++ {
		lines[enabledIdx[i]] = "+" + reversedNames[i]
	}

	var sb strings.Builder
	for _, line := range lines {
		sb.WriteString(line)
		sb.WriteByte('\n')
	}

	return os.WriteFile(dest, []byte(sb.String()), 0o644)
}

// modsInDisplayOrder returns mods in the order the UI shows them:
// modlistOrder entries first (skipping missing), then any unlisted mods.
// Falls back to result.Mods when no modlist is loaded.
func (a *App) modsInDisplayOrder() []*conflict.ModInfo {
	if len(a.modlistOrder) == 0 {
		return a.result.Mods
	}
	modByName := make(map[string]*conflict.ModInfo, len(a.result.Mods))
	for _, m := range a.result.Mods {
		modByName[m.Name] = m
	}
	mods := make([]*conflict.ModInfo, 0, len(a.result.Mods))
	for _, n := range a.modlistOrder {
		if m, ok := modByName[n]; ok {
			mods = append(mods, m)
		}
	}
	for _, m := range a.result.Mods {
		if !a.modlistSet[m.Name] {
			mods = append(mods, m)
		}
	}

	return mods
}

// completeModlistOrder returns the full order displayed by the UI, including
// archives that are on disk but not yet listed in modlist.txt. Missing entries
// already present in modlist.txt are preserved in place.
func (a *App) completeModlistOrder() []string {
	order := append([]string(nil), a.modlistOrder...)
	seen := make(map[string]bool, len(order))
	for _, name := range order {
		seen[name] = true
	}
	for _, m := range a.result.Mods {
		if !seen[m.Name] {
			order = append(order, m.Name)
			seen[m.Name] = true
		}
	}

	return order
}

// setModlistOrder makes the proposed modlist order and the internal priorities
// describe the same load order, then recomputes conflict wins and losses.
func (a *App) setModlistOrder(names []string) {
	a.modlistOrder = append([]string(nil), names...)
	a.modlistSet = make(map[string]bool, len(names))
	for _, name := range names {
		a.modlistSet[name] = true
	}

	modByName := make(map[string]*conflict.ModInfo, len(a.result.Mods))
	for _, m := range a.result.Mods {
		m.Priority = 0
		modByName[m.Name] = m
	}

	a.cfg.Priorities = make(map[string]int, len(a.result.Mods))
	for i, name := range names {
		if m, ok := modByName[name]; ok {
			m.Priority = i + 1
			a.cfg.Priorities[name] = i + 1
		}
	}
	_ = a.cfg.Save(a.cfgPath)
	a.result.ApplyPriorities()
}

// ---- Private helpers -------------------------------------------------------

// buildScanResult converts internal state to a ScanResultDTO.
// Row order follows modlist.txt when present; otherwise result.Mods order.
func (a *App) buildScanResult() ScanResultDTO {
	if a.result == nil {
		return ScanResultDTO{}
	}

	var rows []DisplayRowDTO

	if len(a.modlistOrder) == 0 {
		// No modlist.txt — use result.Mods order.
		for _, m := range a.result.Mods {
			rows = append(rows, DisplayRowDTO{Mod: modToDTO(m, a.pathMap), Name: m.Name})
		}
	} else {
		modByName := make(map[string]*conflict.ModInfo, len(a.result.Mods))
		for _, m := range a.result.Mods {
			modByName[m.Name] = m
		}
		// modlist.txt entries first.
		for _, name := range a.modlistOrder {
			if m, ok := modByName[name]; ok {
				rows = append(rows, DisplayRowDTO{Mod: modToDTO(m, a.pathMap), Name: name})
			} else {
				rows = append(rows, DisplayRowDTO{Mod: nil, Name: name, Missing: true})
			}
		}
		// Unlisted mods (on disk, not in modlist.txt) at the bottom.
		for _, m := range a.result.Mods {
			if !a.modlistSet[m.Name] {
				rows = append(rows, DisplayRowDTO{Mod: modToDTO(m, a.pathMap), Name: m.Name, Unlisted: true})
			}
		}
	}

	// Build conflict list, resolving hex hashes to human-readable paths where available.
	conflicts := make([]ConflictDTO, 0, len(a.result.Conflicts))
	for _, ce := range a.result.Conflicts {
		mods := make([]string, len(ce.Mods))
		for i, m := range ce.Mods {
			mods[i] = m.Name
		}
		res := ce.Resource
		if human, ok := a.pathMap[res]; ok {
			res = human
		}
		conflicts = append(conflicts, ConflictDTO{Resource: res, Mods: mods})
	}

	return ScanResultDTO{
		Rows:       rows,
		Conflicts:  conflicts,
		Summary:    a.result.Summary(),
		HasModlist: len(a.modlistOrder) > 0,
	}
}

// modToDTO converts a ModInfo to ModDTO, capping ConflictsWith at 50 entries.
// pathMap resolves hex resource keys to human-readable paths where available.
func modToDTO(m *conflict.ModInfo, pathMap map[string]string) *ModDTO {
	pairs := m.ConflictsWith
	hasMore := len(pairs) > 50
	moreCount := 0
	if hasMore {
		moreCount = len(pairs) - 50
		pairs = pairs[:50]
	}

	cw := make([]ConflictPairDTO, len(pairs))
	for i, p := range pairs {
		res := p.Resource
		if human, ok := pathMap[res]; ok {
			res = human
		}
		cw[i] = ConflictPairDTO{Opponent: p.Opponent.Name, Resource: res}
	}

	return &ModDTO{
		Name:          m.Name,
		FileCount:     m.FileCount,
		Priority:      m.Priority,
		ConflictCount: m.ConflictCount,
		Wins:          m.Wins,
		Losses:        m.Losses,
		ConflictsWith: cw,
		HasMore:       hasMore,
		MoreCount:     moreCount,
		HasXL:         m.HasXL,
	}
}

// readModlistOrder reads modlist.txt from dir and returns the ordered mod names.
// Returns nil (not an error) when the file does not exist.
func (a *App) readModlistOrder(dir string) ([]string, error) {
	data, err := os.ReadFile(filepath.Join(dir, "modlist.txt"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var names []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			names = append(names, line)
		}
	}

	return names, nil
}

// GroupConflicts moves each connected conflict component into a contiguous block
// at the position of the component's last member in the current list.
// Relative order within the component is preserved. Non-related mods are untouched.
// A "re-order"-tagged backup of the current modlist.txt is written first.
func (a *App) GroupConflicts() (ScanResultDTO, error) {
	if a.result == nil {
		return ScanResultDTO{}, fmt.Errorf("no scan results — run a scan first")
	}
	if a.modDir == "" {
		return ScanResultDTO{}, fmt.Errorf("no mod directory set")
	}

	components := a.conflictComponents()
	if len(components) == 0 {
		return a.buildScanResult(), nil
	}

	modByName := make(map[string]*conflict.ModInfo, len(a.result.Mods))
	for _, m := range a.result.Mods {
		modByName[m.Name] = m
	}

	order := make([]string, len(a.modlistOrder))
	copy(order, a.modlistOrder)

	for _, comp := range components {
		// Build set of names present in the current order.
		compSet := make(map[string]bool, len(comp))
		for _, m := range comp {
			if _, inOrder := indexOf(order, m.Name); inOrder {
				compSet[m.Name] = true
			}
		}
		if len(compSet) < 2 {
			continue
		}

		// Find the group member whose name is last alphabetically — that is the anchor.
		var alphaLast string
		for n := range compSet {
			if n > alphaLast {
				alphaLast = n
			}
		}
		anchorPos, _ := indexOf(order, alphaLast)

		// Collect group members in their current relative order.
		var groupInOrder []string
		for _, n := range order {
			if compSet[n] {
				groupInOrder = append(groupInOrder, n)
			}
		}

		// Rebuild: walk the list, skip group members, insert the whole group at anchorPos.
		newOrder := make([]string, 0, len(order))
		for i, n := range order {
			if compSet[n] {
				if i == anchorPos {
					newOrder = append(newOrder, groupInOrder...)
				}
				// else drop — already captured in groupInOrder
			} else {
				newOrder = append(newOrder, n)
			}
		}
		order = newOrder
	}

	mods := make([]*conflict.ModInfo, 0, len(order))
	for _, n := range order {
		if m, ok := modByName[n]; ok {
			mods = append(mods, m)
		}
	}
	if err := modlist.Write(a.modDir, mods, "re-order"); err != nil {
		return ScanResultDTO{}, fmt.Errorf("write grouped modlist: %w", err)
	}
	_ = modlist.PruneBackups(filepath.Join(a.modDir, "modlist.old"), a.cfg.EffectiveBackupLimit())

	a.modlistOrder = order
	a.modlistSet = make(map[string]bool, len(order))
	for _, n := range order {
		a.modlistSet[n] = true
	}

	return a.buildScanResult(), nil
}

// indexOf returns the index of name in slice and whether it was found.
func indexOf(slice []string, name string) (int, bool) {
	for i, n := range slice {
		if n == name {
			return i, true
		}
	}

	return -1, false
}

// conflictComponents returns all connected components of the conflict graph.
// Each component is a slice of mods that are transitively connected by conflicts.
func (a *App) conflictComponents() [][]*conflict.ModInfo {
	adj := make(map[*conflict.ModInfo]map[*conflict.ModInfo]bool)
	for _, ce := range a.result.Conflicts {
		for _, m := range ce.Mods {
			if adj[m] == nil {
				adj[m] = make(map[*conflict.ModInfo]bool)
			}
			for _, other := range ce.Mods {
				if other != m {
					adj[m][other] = true
				}
			}
		}
	}

	seen := make(map[*conflict.ModInfo]bool)
	var components [][]*conflict.ModInfo
	for _, m := range a.result.Mods {
		if seen[m] || len(adj[m]) == 0 {
			continue
		}
		var comp []*conflict.ModInfo
		queue := []*conflict.ModInfo{m}
		seen[m] = true
		for len(queue) > 0 {
			cur := queue[0]
			queue = queue[1:]
			comp = append(comp, cur)
			for nb := range adj[cur] {
				if !seen[nb] {
					seen[nb] = true
					queue = append(queue, nb)
				}
			}
		}
		components = append(components, comp)
	}

	return components
}

// conflictGroup returns all mods that share at least one conflicting resource with m,
// sorted by their current position in result.Mods (load order).
func (a *App) conflictGroup(m *conflict.ModInfo) []*conflict.ModInfo {
	seen := make(map[*conflict.ModInfo]bool)
	for _, ce := range a.result.Conflicts {
		if slices.Contains(ce.Mods, m) {
			for _, cm := range ce.Mods {
				seen[cm] = true
			}
		}
	}
	out := make([]*conflict.ModInfo, 0, len(seen))
	for mod := range seen {
		out = append(out, mod)
	}
	posOf := make(map[*conflict.ModInfo]int, len(a.result.Mods))
	for i, mod := range a.result.Mods {
		posOf[mod] = i
	}
	sort.Slice(out, func(i, j int) bool {
		return posOf[out[i]] < posOf[out[j]]
	})

	return out
}
