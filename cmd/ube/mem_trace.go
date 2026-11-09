package main

import (
	"fmt"
	"os"
	"sort"

	"github.com/bytecodealliance/wasmtime-go/v29"
)

type ScopeKind int32

const (
	ScopeKindFunc ScopeKind = iota
	ScopeKindArena
	ScopeKindLoop
)

type ScopeMeta struct {
	ID        int32
	Name      string
	File      string
	Line      int
	Kind      ScopeKind
	ParentID  int32
}

type ScopeStat struct {
	Meta         ScopeMeta
	EnterCount   int64
	PeakAlloc    int64
	CurrentAlloc int64
	Promoted     int64
	Reclaimed    int64
}

type SliceWasteStat struct {
	OldCap   int32
	NewCap   int32
	ElemSize int32
	Waste    int64
	Line     int
}

type LoopChurnWarning struct {
	LoopID     int32
	ScopeMeta  ScopeMeta
	Iterations int64
	AllocBytes int64
}

type MemoryTracker struct {
	Enabled       bool
	Scopes        map[int32]ScopeMeta
	ScopeStats    map[int32]*ScopeStat
	ScopeStack    []int32
	LoopStack     []int32
	LoopIterCount map[int32]int64
	LoopAllocSum  map[int32]int64

	TotalAllocBytes int64
	PeakLinearBytes int64
	OrphanedWaste   int64
	SliceWastes     []SliceWasteStat

	LinearPages uint32
	StaticData  uint32
	BaseHeap    uint32

	CurrentBump int64
	PeakBump    int64
}

func newMemoryTracker() *MemoryTracker {
	return &MemoryTracker{
		Enabled:       true,
		Scopes:        make(map[int32]ScopeMeta),
		ScopeStats:    make(map[int32]*ScopeStat),
		LoopIterCount: make(map[int32]int64),
		LoopAllocSum:  make(map[int32]int64),
		StaticData:    64,
		BaseHeap:      4096,
	}
}

func (t *MemoryTracker) currentScopeID() int32 {
	if len(t.ScopeStack) == 0 {
		return 0
	}
	return t.ScopeStack[len(t.ScopeStack)-1]
}

func (t *MemoryTracker) getOrCreateStat(scopeID int32) *ScopeStat {
	st, ok := t.ScopeStats[scopeID]
	if !ok {
		meta := t.Scopes[scopeID]
		st = &ScopeStat{Meta: meta}
		t.ScopeStats[scopeID] = st
	}
	return st
}

func (t *MemoryTracker) EnterScope(scopeID int32, kind int32) {
	t.ScopeStack = append(t.ScopeStack, scopeID)
	st := t.getOrCreateStat(scopeID)
	st.EnterCount++
}

func (t *MemoryTracker) ExitScope(scopeID int32) {
	if len(t.ScopeStack) > 0 && t.ScopeStack[len(t.ScopeStack)-1] == scopeID {
		t.ScopeStack = t.ScopeStack[:len(t.ScopeStack)-1]
	}
	st := t.getOrCreateStat(scopeID)
	st.Reclaimed += st.CurrentAlloc
	st.CurrentAlloc = 0
}

func (t *MemoryTracker) TrackAlloc(size int32, kind int32) {
	if size <= 0 {
		return
	}
	sz := int64(size)
	t.TotalAllocBytes += sz
	t.CurrentBump += sz
	if t.CurrentBump > t.PeakBump {
		t.PeakBump = t.CurrentBump
	}
	if len(t.ScopeStack) > 0 {
		curID := t.ScopeStack[len(t.ScopeStack)-1]
		st := t.getOrCreateStat(curID)
		st.CurrentAlloc += sz
		if st.CurrentAlloc > st.PeakAlloc {
			st.PeakAlloc = st.CurrentAlloc
		}
	}
	if len(t.LoopStack) > 0 {
		innermostLoop := t.LoopStack[len(t.LoopStack)-1]
		if len(t.ScopeStack) > 0 {
			curScope := t.ScopeStack[len(t.ScopeStack)-1]
			if curScope == innermostLoop {
				t.LoopAllocSum[innermostLoop] += sz
			}
		}
	}
}

func (t *MemoryTracker) TrackPromote(size int32) {
	if size <= 0 {
		return
	}
	if len(t.ScopeStack) > 0 {
		curID := t.ScopeStack[len(t.ScopeStack)-1]
		st := t.getOrCreateStat(curID)
		st.Promoted += int64(size)
	}
}

func (t *MemoryTracker) TrackSliceRealloc(oldCap, newCap, elemSize int32) {
	waste := int64(oldCap * elemSize)
	t.OrphanedWaste += waste
	t.SliceWastes = append(t.SliceWastes, SliceWasteStat{
		OldCap:   oldCap,
		NewCap:   newCap,
		ElemSize: elemSize,
		Waste:    waste,
	})
}

func (t *MemoryTracker) TrackLoopIter(loopID int32) {
	t.LoopIterCount[loopID]++
}

func (t *MemoryTracker) EnterLoop(loopID int32) {
	t.LoopStack = append(t.LoopStack, loopID)
}

func (t *MemoryTracker) ExitLoop(loopID int32) {
	if len(t.LoopStack) > 0 && t.LoopStack[len(t.LoopStack)-1] == loopID {
		t.LoopStack = t.LoopStack[:len(t.LoopStack)-1]
	}
}

func formatBytes(b int64) string {
	if b < 1024 {
		return fmt.Sprintf("%d B", b)
	} else if b < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(b)/1024.0)
	}
	return fmt.Sprintf("%.2f MB", float64(b)/(1024.0*1024.0))
}

func (t *MemoryTracker) PrintReport(ai bool) {
	if t == nil {
		return
	}
	totalCommitted := int64(t.LinearPages) * 65536
	if totalCommitted == 0 {
		totalCommitted = 2 * 65536
	}
	peakUsed := int64(t.BaseHeap) + t.PeakBump
	pctUsed := float64(peakUsed) / float64(totalCommitted) * 100.0

	var runawayLoops []LoopChurnWarning
	for loopID, iter := range t.LoopIterCount {
		allocBytes := t.LoopAllocSum[loopID]
		if iter >= 10 && allocBytes > 4096 {
			meta := t.Scopes[loopID]
			runawayLoops = append(runawayLoops, LoopChurnWarning{
				LoopID:     loopID,
				ScopeMeta:  meta,
				Iterations: iter,
				AllocBytes: allocBytes,
			})
		}
	}

	if ai {
		for _, w := range runawayLoops {
			loc := fmt.Sprintf("%s:%d:1", w.ScopeMeta.File, w.ScopeMeta.Line)
			fmt.Fprintf(os.Stderr, "WARN|%s|runaway_arena_loop|allocated_bytes:%d|iterations:%d|hint:wrap loop body in arena block\n",
				loc, w.AllocBytes, w.Iterations)
		}
		if t.OrphanedWaste > 0 {
			fmt.Fprintf(os.Stderr, "PERF|mem_trace|slice_geometric_waste|wasted_bytes:%d|hint:preallocate capacity\n",
				t.OrphanedWaste)
		}
		return
	}

	fmt.Println()
	fmt.Println("=========================== UBE MEMORY AUDIT ===========================")
	fmt.Println("LINEAR MEMORY:")
	fmt.Printf("  Base Static Data:         %s\n", formatBytes(int64(t.StaticData)))
	fmt.Printf("  Wasm Memory Pages:        %d pages (%s total committed)\n", t.LinearPages, formatBytes(totalCommitted))
	fmt.Printf("  Peak Linear Memory Used:  %s (%.1f%% of committed)\n\n", formatBytes(peakUsed), pctUsed)

	fmt.Println("REGIONAL ARENAS:")
	fmt.Printf("  %-32s %10s %10s %10s %11s\n", "Scope", "Peak Used", "Promoted", "Reclaimed", "Efficiency")
	fmt.Println("  -------------------------------------------------------------------------")

	var sortedStats []*ScopeStat
	for _, st := range t.ScopeStats {
		if st.Meta.Kind == ScopeKindLoop {
			continue
		}
		sortedStats = append(sortedStats, st)
	}
	sort.Slice(sortedStats, func(i, j int) bool {
		return sortedStats[i].PeakAlloc > sortedStats[j].PeakAlloc
	})

	for _, st := range sortedStats {
		name := st.Meta.Name
		if name == "" {
			name = "anonymous"
		}
		if st.Meta.Line > 0 {
			name = fmt.Sprintf("%s:%d [%s]", st.Meta.File, st.Meta.Line, name)
		}
		if len(name) > 32 {
			name = name[:29] + "..."
		}
		eff := 100.0
		totalExited := st.Reclaimed + st.Promoted
		if totalExited > 0 {
			eff = (float64(st.Reclaimed) / float64(totalExited)) * 100.0
		}
		fmt.Printf("  %-32s %10s %10s %10s %10.1f%%\n",
			name,
			formatBytes(st.PeakAlloc),
			formatBytes(st.Promoted),
			formatBytes(st.Reclaimed),
			eff,
		)
	}

	if len(runawayLoops) > 0 {
		fmt.Println()
		for _, w := range runawayLoops {
			fmt.Printf("  [!] WARNING: Runaway bump detected in loop at %s:%d!\n", w.ScopeMeta.File, w.ScopeMeta.Line)
			fmt.Printf("      Allocated %s across %d iterations without an inner arena.\n",
				formatBytes(w.AllocBytes), w.Iterations)
			fmt.Println("      Suggestion: Wrap loop body in 'arena { ... }' or hoist allocations.")
		}
	}

	fmt.Println()
	fmt.Println("SLICING & RESIZE WASTE:")
	fmt.Printf("  Orphaned Buffer Waste:  %s (due to geometric capacity expansions)\n", formatBytes(t.OrphanedWaste))
	if len(t.SliceWastes) > 0 {
		largest := t.SliceWastes[0]
		for _, sw := range t.SliceWastes {
			if sw.Waste > largest.Waste {
				largest = sw
			}
		}
		fmt.Printf("  Largest uncompacted realloc: capacity grown %d -> %d elements (%s dead)\n",
			largest.OldCap, largest.NewCap, formatBytes(largest.Waste))
	}
	fmt.Println("========================================================================")
}

func registerMemTraceLinker(linker *wasmtime.Linker, store *wasmtime.Store, tracker *MemoryTracker) error {
	fnEnter := wasmtime.WrapFunc(store, func(caller *wasmtime.Caller, scopeID int32, kind int32) {
		tracker.EnterScope(scopeID, kind)
	})
	if err := linker.Define(store, "ube_trace", "arena_enter", fnEnter); err != nil {
		return err
	}

	fnExit := wasmtime.WrapFunc(store, func(caller *wasmtime.Caller, scopeID int32) {
		tracker.ExitScope(scopeID)
	})
	if err := linker.Define(store, "ube_trace", "arena_exit", fnExit); err != nil {
		return err
	}

	fnAlloc := wasmtime.WrapFunc(store, func(caller *wasmtime.Caller, size int32, kind int32) {
		tracker.TrackAlloc(size, kind)
	})
	if err := linker.Define(store, "ube_trace", "arena_alloc", fnAlloc); err != nil {
		return err
	}

	fnPromote := wasmtime.WrapFunc(store, func(caller *wasmtime.Caller, size int32) {
		tracker.TrackPromote(size)
	})
	if err := linker.Define(store, "ube_trace", "arena_promote", fnPromote); err != nil {
		return err
	}

	fnSliceRealloc := wasmtime.WrapFunc(store, func(caller *wasmtime.Caller, oldCap int32, newCap int32, elemSize int32) {
		tracker.TrackSliceRealloc(oldCap, newCap, elemSize)
	})
	if err := linker.Define(store, "ube_trace", "slice_realloc", fnSliceRealloc); err != nil {
		return err
	}

	fnLoopIter := wasmtime.WrapFunc(store, func(caller *wasmtime.Caller, loopID int32) {
		tracker.TrackLoopIter(loopID)
	})
	if err := linker.Define(store, "ube_trace", "loop_iter", fnLoopIter); err != nil {
		return err
	}

	fnLoopEnter := wasmtime.WrapFunc(store, func(caller *wasmtime.Caller, loopID int32) {
		tracker.EnterLoop(loopID)
	})
	if err := linker.Define(store, "ube_trace", "loop_enter", fnLoopEnter); err != nil {
		return err
	}

	fnLoopExit := wasmtime.WrapFunc(store, func(caller *wasmtime.Caller, loopID int32) {
		tracker.ExitLoop(loopID)
	})
	if err := linker.Define(store, "ube_trace", "loop_exit", fnLoopExit); err != nil {
		return err
	}

	return nil
}
