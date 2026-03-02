package result

import (
	"sort"
	"sync"

	"github.com/oisee/6502-optimizer/pkg/inst"
)

// Rule represents a single optimization rule.
type Rule struct {
	Source      []inst.Instruction
	Replacement []inst.Instruction
	BytesSaved  int
	CyclesSaved int
	DeadFlags   uint8 // 0 = unconditional, nonzero = flags that must be dead
}

// Table is a thread-safe collection of optimization rules.
type Table struct {
	mu    sync.Mutex
	rules []Rule
}

// NewTable creates an empty rule table.
func NewTable() *Table {
	return &Table{}
}

// Add appends a rule to the table.
func (t *Table) Add(r Rule) {
	t.mu.Lock()
	t.rules = append(t.rules, r)
	t.mu.Unlock()
}

// Rules returns a sorted copy of all rules (by bytes saved desc, then cycles).
func (t *Table) Rules() []Rule {
	t.mu.Lock()
	result := make([]Rule, len(t.rules))
	copy(result, t.rules)
	t.mu.Unlock()

	sort.Slice(result, func(i, j int) bool {
		if result[i].BytesSaved != result[j].BytesSaved {
			return result[i].BytesSaved > result[j].BytesSaved
		}
		return result[i].CyclesSaved > result[j].CyclesSaved
	})
	return result
}

// Len returns the number of rules.
func (t *Table) Len() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.rules)
}
