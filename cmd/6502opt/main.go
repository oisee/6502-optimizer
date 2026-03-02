package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"

	"github.com/oisee/6502-optimizer/pkg/inst"
	"github.com/oisee/6502-optimizer/pkg/result"
	"github.com/oisee/6502-optimizer/pkg/search"
	"github.com/oisee/6502-optimizer/pkg/stoke"
	"github.com/spf13/cobra"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "6502opt",
		Short: "6502 superoptimizer — find optimal instruction sequences",
	}

	// enumerate command
	var maxTarget int
	var output string
	var checkpoint string
	var verbose bool
	var numWorkers int
	var deadFlagsStr string

	enumCmd := &cobra.Command{
		Use:   "enumerate",
		Short: "Enumerate all target sequences and find shorter replacements",
		RunE: func(cmd *cobra.Command, args []string) error {
			deadFlags, err := parseDeadFlags(deadFlagsStr)
			if err != nil {
				return err
			}

			fmt.Printf("6502 Superoptimizer\n")
			fmt.Printf("  Max target length: %d\n", maxTarget)
			fmt.Printf("  Instructions: %d per position\n", search.InstructionCount())
			fmt.Printf("  Workers: %d\n", numWorkers)
			if deadFlags != 0 {
				fmt.Printf("  Dead flags: 0x%02X (%s)\n", deadFlags, result.DeadFlagDesc(deadFlags))
			}
			fmt.Println()

			cfg := search.Config{
				MaxTargetLen: maxTarget,
				NumWorkers:   numWorkers,
				Verbose:      verbose,
				DeadFlags:    deadFlags,
			}
			table := search.Run(cfg)
			rules := table.Rules()

			fmt.Printf("\nFound %d optimizations\n", len(rules))

			if output != "" {
				f, err := os.Create(output)
				if err != nil {
					return err
				}
				defer f.Close()
				if err := result.WriteJSON(f, rules); err != nil {
					return err
				}
				fmt.Printf("Written to %s\n", output)
			}

			_ = checkpoint // TODO: implement checkpoint resume
			return nil
		},
	}
	enumCmd.Flags().IntVar(&maxTarget, "max-target", 2, "Maximum target sequence length")
	enumCmd.Flags().StringVar(&output, "output", "", "Output JSON file path")
	enumCmd.Flags().StringVar(&checkpoint, "checkpoint", "", "Checkpoint file for resume")
	enumCmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Verbose output")
	enumCmd.Flags().IntVar(&numWorkers, "workers", runtime.NumCPU(), "Number of workers")
	enumCmd.Flags().StringVar(&deadFlagsStr, "dead-flags", "none", "Dead flags mask: none, all, or hex (e.g. 0xFF)")

	// target command
	var maxCand int

	targetCmd := &cobra.Command{
		Use:   "target [instructions]",
		Short: "Find optimal replacement for a specific instruction sequence",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			deadFlags, err := parseDeadFlags(deadFlagsStr)
			if err != nil {
				return err
			}

			input := strings.Join(args, " ")
			seq, err := parseAssembly(input)
			if err != nil {
				return fmt.Errorf("failed to parse: %w", err)
			}

			fmt.Printf("Target: %s (%d bytes, %d cycles)\n",
				input, inst.SeqByteSize(seq), inst.SeqCycles(seq))

			var rule *result.Rule
			if deadFlags != 0 {
				rule = search.SearchSingleMasked(seq, maxCand, deadFlags, verbose)
			} else {
				rule = search.SearchSingle(seq, maxCand, verbose)
			}

			if rule == nil {
				fmt.Println("No shorter replacement found.")
				return nil
			}

			fmt.Printf("Replacement: ")
			for i, instr := range rule.Replacement {
				if i > 0 {
					fmt.Print(" : ")
				}
				fmt.Print(inst.Disassemble(instr))
			}
			fmt.Printf(" (-%d bytes, -%d cycles)\n", rule.BytesSaved, rule.CyclesSaved)
			return nil
		},
	}
	targetCmd.Flags().IntVar(&maxCand, "max-candidate", 4, "Maximum candidate length")
	targetCmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Verbose output")
	targetCmd.Flags().StringVar(&deadFlagsStr, "dead-flags", "none", "Dead flags mask: none, all, or hex (e.g. 0xFF)")

	// verify-jsonl command
	var verifyDeadFlagsStr string
	verifyJSONLCmd := &cobra.Command{
		Use:   "verify-jsonl [file.jsonl]",
		Short: "Verify JSONL rules using CPU ExhaustiveCheck",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return verifyJSONL(args[0], verifyDeadFlagsStr, verbose)
		},
	}
	verifyJSONLCmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Verbose output")
	verifyJSONLCmd.Flags().StringVar(&verifyDeadFlagsStr, "dead-flags", "none", "Dead flags mask for verification")

	// export command
	var format string

	exportCmd := &cobra.Command{
		Use:   "export [rules.json]",
		Short: "Export rules in various formats",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			f, err := os.Open(args[0])
			if err != nil {
				return err
			}
			defer f.Close()

			jrules, err := result.ReadJSON(f)
			if err != nil {
				return err
			}

			switch format {
			case "go":
				// Re-parse JSON rules into internal rules
				rules := make([]result.Rule, 0, len(jrules))
				for _, jr := range jrules {
					src, err := parseAssemblySlice(jr.Source)
					if err != nil {
						fmt.Fprintf(os.Stderr, "skipping rule (bad source): %v\n", err)
						continue
					}
					rep, err := parseAssemblySlice(jr.Replacement)
					if err != nil {
						fmt.Fprintf(os.Stderr, "skipping rule (bad replacement): %v\n", err)
						continue
					}
					rules = append(rules, result.Rule{
						Source:      src,
						Replacement: rep,
						BytesSaved:  jr.BytesSaved,
						CyclesSaved: jr.CyclesSaved,
					})
				}
				return result.WriteGoCode(os.Stdout, rules)
			default:
				return fmt.Errorf("unknown format: %s", format)
			}
		},
	}
	exportCmd.Flags().StringVarP(&format, "format", "f", "go", "Output format (go)")

	// stoke command
	var stokeChains int
	var stokeIter int
	var stokeDecay float64
	var stokeOutput string
	var stokeVerbose bool
	var stokeDeadFlagsStr string

	stokeCmd := &cobra.Command{
		Use:   "stoke",
		Short: "Run STOKE stochastic superoptimizer on a target sequence",
		RunE: func(cmd *cobra.Command, args []string) error {
			targetStr, _ := cmd.Flags().GetString("target")
			if targetStr == "" {
				return fmt.Errorf("--target is required")
			}
			seq, err := parseAssembly(targetStr)
			if err != nil {
				return fmt.Errorf("failed to parse target: %w", err)
			}

			deadFlags, err := parseDeadFlags(stokeDeadFlagsStr)
			if err != nil {
				return err
			}

			cfg := stoke.Config{
				Target:     seq,
				Chains:     stokeChains,
				Iterations: stokeIter,
				Decay:      stokeDecay,
				Verbose:    stokeVerbose,
				DeadFlags:  deadFlags,
			}

			results := stoke.Run(cfg)
			results = stoke.Deduplicate(results)

			fmt.Printf("\n%d unique optimizations found\n", len(results))
			for i, r := range results {
				fmt.Printf("  %d. ", i+1)
				for j, instr := range r.Rule.Replacement {
					if j > 0 {
						fmt.Print(" : ")
					}
					fmt.Print(inst.Disassemble(instr))
				}
				if r.Rule.DeadFlags != 0 {
					fmt.Printf(" (-%d bytes, -%d cycles, dead flags: %s)\n",
						r.Rule.BytesSaved, r.Rule.CyclesSaved, result.DeadFlagDesc(r.Rule.DeadFlags))
				} else {
					fmt.Printf(" (-%d bytes, -%d cycles)\n",
						r.Rule.BytesSaved, r.Rule.CyclesSaved)
				}
			}

			if stokeOutput != "" && len(results) > 0 {
				rules := make([]result.Rule, len(results))
				for i, r := range results {
					rules[i] = r.Rule
				}
				f, err := os.Create(stokeOutput)
				if err != nil {
					return err
				}
				defer f.Close()
				if err := result.WriteJSON(f, rules); err != nil {
					return err
				}
				fmt.Printf("Written to %s\n", stokeOutput)
			}
			return nil
		},
	}
	stokeCmd.Flags().String("target", "", "Target assembly sequence (colon-separated)")
	stokeCmd.Flags().IntVar(&stokeChains, "chains", runtime.NumCPU(), "Number of MCMC chains")
	stokeCmd.Flags().IntVar(&stokeIter, "iterations", 10_000_000, "Iterations per chain")
	stokeCmd.Flags().Float64Var(&stokeDecay, "decay", 0.9999, "Temperature decay factor")
	stokeCmd.Flags().StringVar(&stokeOutput, "output", "", "Output JSON file path")
	stokeCmd.Flags().BoolVarP(&stokeVerbose, "verbose", "v", false, "Verbose output")
	stokeCmd.Flags().StringVar(&stokeDeadFlagsStr, "dead-flags", "none", "Dead flags mask: none, all, or hex (e.g. 0xFF)")

	rootCmd.AddCommand(enumCmd, targetCmd, verifyJSONLCmd, exportCmd, stokeCmd)
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// parseDeadFlags parses the --dead-flags flag value.
func parseDeadFlags(s string) (search.FlagMask, error) {
	switch strings.ToLower(s) {
	case "none", "":
		return search.DeadNone, nil
	case "all":
		return search.DeadAll, nil
	default:
		s = strings.TrimPrefix(strings.ToLower(s), "0x")
		v, err := strconv.ParseUint(s, 16, 8)
		if err != nil {
			return 0, fmt.Errorf("invalid --dead-flags value %q: use none, all, or hex (e.g. 0xFF)", s)
		}
		return search.FlagMask(v), nil
	}
}

// parseAssembly converts assembly text like "LDA #$42 : TAX" into instructions.
func parseAssembly(text string) ([]inst.Instruction, error) {
	parts := strings.Split(text, ":")
	var seq []inst.Instruction

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		instr, err := parseSingleInstruction(part)
		if err != nil {
			return nil, fmt.Errorf("cannot parse %q: %w", part, err)
		}
		seq = append(seq, instr)
	}

	if len(seq) == 0 {
		return nil, fmt.Errorf("no instructions parsed from %q", text)
	}
	return seq, nil
}

// parseAssemblySlice parses a slice of individual assembly strings.
func parseAssemblySlice(asms []string) ([]inst.Instruction, error) {
	seq := make([]inst.Instruction, len(asms))
	for i, s := range asms {
		instr, err := parseSingleInstruction(s)
		if err != nil {
			return nil, fmt.Errorf("cannot parse %q: %w", s, err)
		}
		seq[i] = instr
	}
	return seq, nil
}

func parseSingleInstruction(text string) (inst.Instruction, error) {
	text = strings.TrimSpace(text)
	upper := strings.ToUpper(text)

	for op := inst.OpCode(0); op < inst.OpCodeCount; op++ {
		info := &inst.Catalog[op]
		if info.Mnemonic == "" {
			continue
		}

		if !inst.HasImmediate(op) {
			if strings.EqualFold(text, info.Mnemonic) {
				return inst.Instruction{Op: op}, nil
			}
			continue
		}

		// For immediate instructions, mnemonic has "n" as placeholder (e.g. "LDA #n")
		pattern := strings.ToUpper(info.Mnemonic)
		nIdx := strings.LastIndex(pattern, "N")
		if nIdx < 0 {
			continue
		}
		prefix := pattern[:nIdx]
		suffix := pattern[nIdx+1:]

		if !strings.HasPrefix(upper, prefix) {
			continue
		}
		if suffix != "" && !strings.HasSuffix(upper, suffix) {
			continue
		}

		valStr := upper[len(prefix):]
		if suffix != "" {
			valStr = valStr[:len(valStr)-len(suffix)]
		}
		valStr = strings.TrimSpace(valStr)

		val, err := parseImmediate(valStr)
		if err != nil {
			continue
		}
		return inst.Instruction{Op: op, Imm: uint16(val)}, nil
	}

	return inst.Instruction{}, fmt.Errorf("unknown instruction: %s", text)
}

func verifyJSONL(path string, deadFlagsStr string, verbose bool) error {
	deadFlags, err := parseDeadFlags(deadFlagsStr)
	if err != nil {
		return err
	}

	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	total, passed, failed, skipped := 0, 0, 0, 0

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		total++

		var rule struct {
			SourceASM      string `json:"source_asm"`
			ReplacementASM string `json:"replacement_asm"`
			BytesSaved     int    `json:"bytes_saved"`
			CyclesSaved    int    `json:"cycles_saved"`
		}
		if err := json.Unmarshal([]byte(line), &rule); err != nil {
			fmt.Fprintf(os.Stderr, "  [%d] JSON parse error: %v\n", total, err)
			skipped++
			continue
		}

		source, err := parseAssembly(rule.SourceASM)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  [%d] Cannot parse source %q: %v\n", total, rule.SourceASM, err)
			skipped++
			continue
		}
		replacement, err := parseAssembly(rule.ReplacementASM)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  [%d] Cannot parse replacement %q: %v\n", total, rule.ReplacementASM, err)
			skipped++
			continue
		}

		var ok bool
		if deadFlags == search.DeadNone {
			ok = search.ExhaustiveCheck(source, replacement)
		} else {
			ok = search.ExhaustiveCheckMasked(source, replacement, deadFlags)
		}

		if ok {
			passed++
			if verbose {
				fmt.Printf("  [%d] PASS: %s -> %s\n", total, rule.SourceASM, rule.ReplacementASM)
			}
		} else {
			failed++
			fmt.Printf("  [%d] FAIL: %s -> %s\n", total, rule.SourceASM, rule.ReplacementASM)
		}

		if total%10000 == 0 {
			fmt.Fprintf(os.Stderr, "  Progress: %d verified (%d pass, %d fail, %d skip)\n",
				total, passed, failed, skipped)
		}
	}

	fmt.Printf("\nVerification complete: %d total, %d passed, %d failed, %d skipped\n",
		total, passed, failed, skipped)
	if failed > 0 {
		return fmt.Errorf("%d rules failed verification", failed)
	}
	return nil
}

// parseImmediate parses a hex or decimal immediate value.
// Supports: $FF, 0xFF, 255
func parseImmediate(s string) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty")
	}

	// Handle 6502-style hex: $FF
	if strings.HasPrefix(s, "$") {
		v, err := strconv.ParseUint(s[1:], 16, 16)
		return int(v), err
	}

	// Handle C-style hex: 0xFF
	if strings.HasPrefix(s, "0X") || strings.HasPrefix(s, "0x") {
		v, err := strconv.ParseUint(s[2:], 16, 16)
		return int(v), err
	}

	// Handle Z80-style hex: FFH (for compatibility)
	if strings.HasSuffix(s, "H") {
		v, err := strconv.ParseUint(s[:len(s)-1], 16, 16)
		return int(v), err
	}

	// Decimal
	v, err := strconv.ParseUint(s, 10, 16)
	return int(v), err
}
