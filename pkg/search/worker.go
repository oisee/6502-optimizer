package search

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/oisee/6502-optimizer/pkg/inst"
	"github.com/oisee/6502-optimizer/pkg/result"
)

// WorkerPool manages parallel search workers.
type WorkerPool struct {
	NumWorkers int
	Results    *result.Table
	mu         sync.Mutex
	checked    atomic.Int64
	found      atomic.Int64
	completed  atomic.Int64
}

// NewWorkerPool creates a pool with the given number of workers.
func NewWorkerPool(numWorkers int) *WorkerPool {
	if numWorkers <= 0 {
		numWorkers = runtime.NumCPU()
	}
	return &WorkerPool{
		NumWorkers: numWorkers,
		Results:    result.NewTable(),
	}
}

// SearchTask represents a unit of work.
type SearchTask struct {
	Target     []inst.Instruction
	MaxCandLen int
	DeadFlags  FlagMask
}

// Stats returns search statistics.
func (wp *WorkerPool) Stats() (checked, found int64) {
	return wp.checked.Load(), wp.found.Load()
}

// RunTasks distributes search tasks across workers.
func (wp *WorkerPool) RunTasks(tasks []SearchTask, verbose bool) {
	totalTasks := int64(len(tasks))

	ch := make(chan SearchTask, len(tasks))
	for _, t := range tasks {
		ch <- t
	}
	close(ch)

	done := make(chan struct{})
	startTime := time.Now()
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		var lastChecked int64
		lastTime := startTime
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				now := time.Now()
				comp := wp.completed.Load()
				checked := wp.checked.Load()
				found := wp.found.Load()
				elapsed := now.Sub(startTime)

				dt := now.Sub(lastTime).Seconds()
				dc := checked - lastChecked
				rate := float64(dc) / dt
				lastChecked = checked
				lastTime = now

				var eta string
				if comp > 0 {
					remaining := time.Duration(float64(elapsed) * float64(totalTasks-comp) / float64(comp))
					eta = remaining.Round(time.Second).String()
				} else {
					eta = "..."
				}

				pct := float64(comp) / float64(totalTasks) * 100
				fmt.Printf("  [%s] %d/%d targets (%.1f%%) | %d found | %.1fM checks/s | ETA %s\n",
					elapsed.Round(time.Second), comp, totalTasks, pct, found, rate/1e6, eta)
			}
		}
	}()

	var wg sync.WaitGroup
	for i := 0; i < wp.NumWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for task := range ch {
				wp.processTask(task, verbose)
				wp.completed.Add(1)
			}
		}()
	}
	wg.Wait()

	close(done)
	elapsed := time.Since(startTime)
	comp := wp.completed.Load()
	checked := wp.checked.Load()
	found := wp.found.Load()
	rate := float64(checked) / elapsed.Seconds()
	fmt.Printf("  [%s] %d/%d targets (100.0%%) | %d found | %.1fM checks/s avg | DONE\n",
		elapsed.Round(time.Second), comp, totalTasks, found, rate/1e6)
}

func (wp *WorkerPool) processTask(task SearchTask, verbose bool) {
	targetBytes := inst.SeqByteSize(task.Target)
	targetCycles := inst.SeqCycles(task.Target)

	for candLen := 1; candLen <= task.MaxCandLen; candLen++ {
		found := false
		EnumerateSequences(candLen, func(cand []inst.Instruction) bool {
			wp.checked.Add(1)

			candBytes := inst.SeqByteSize(cand)
			if candBytes >= targetBytes {
				return true
			}

			if ShouldPrune(cand) {
				return true
			}

			if !QuickCheck(task.Target, cand) {
				return true
			}

			if !ExhaustiveCheck(task.Target, cand) {
				return true
			}

			wp.found.Add(1)
			candCopy := make([]inst.Instruction, len(cand))
			copy(candCopy, cand)
			candCycles := inst.SeqCycles(candCopy)

			rule := result.Rule{
				Source:      copySeq(task.Target),
				Replacement: candCopy,
				BytesSaved:  targetBytes - candBytes,
				CyclesSaved: targetCycles - candCycles,
			}

			wp.mu.Lock()
			wp.Results.Add(rule)
			wp.mu.Unlock()

			if verbose {
				fmt.Printf("  FOUND: %s -> %s (-%d bytes, -%d cycles)\n",
					disasmSeq(task.Target), disasmSeq(candCopy),
					rule.BytesSaved, rule.CyclesSaved)
			}

			found = true
			return false
		})
		if found {
			break
		}
	}

	if task.DeadFlags != DeadNone {
		wp.processTaskMasked(task, verbose)
	}
}

func (wp *WorkerPool) processTaskMasked(task SearchTask, verbose bool) {
	targetBytes := inst.SeqByteSize(task.Target)
	targetCycles := inst.SeqCycles(task.Target)

	for candLen := 1; candLen <= task.MaxCandLen; candLen++ {
		found := false
		EnumerateSequences(candLen, func(cand []inst.Instruction) bool {
			wp.checked.Add(1)

			candBytes := inst.SeqByteSize(cand)
			if candBytes >= targetBytes {
				return true
			}

			if ShouldPrune(cand) {
				return true
			}

			if QuickCheck(task.Target, cand) {
				return true
			}

			if !QuickCheckMasked(task.Target, cand, task.DeadFlags) {
				return true
			}

			if !ExhaustiveCheckMasked(task.Target, cand, task.DeadFlags) {
				return true
			}

			flagDiff := FlagDiff(task.Target, cand)
			if flagDiff == 0 {
				return true
			}

			wp.found.Add(1)
			candCopy := make([]inst.Instruction, len(cand))
			copy(candCopy, cand)
			candCycles := inst.SeqCycles(candCopy)

			rule := result.Rule{
				Source:      copySeq(task.Target),
				Replacement: candCopy,
				BytesSaved:  targetBytes - candBytes,
				CyclesSaved: targetCycles - candCycles,
				DeadFlags:   flagDiff,
			}

			wp.mu.Lock()
			wp.Results.Add(rule)
			wp.mu.Unlock()

			if verbose {
				fmt.Printf("  FOUND (dead flags 0x%02X): %s -> %s (-%d bytes, -%d cycles)\n",
					flagDiff, disasmSeq(task.Target), disasmSeq(candCopy),
					rule.BytesSaved, rule.CyclesSaved)
			}

			found = true
			return false
		})
		if found {
			break
		}
	}
}

func copySeq(seq []inst.Instruction) []inst.Instruction {
	c := make([]inst.Instruction, len(seq))
	copy(c, seq)
	return c
}

func disasmSeq(seq []inst.Instruction) string {
	s := ""
	for i, instr := range seq {
		if i > 0 {
			s += " : "
		}
		s += inst.Disassemble(instr)
	}
	return s
}
