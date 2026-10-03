package analysis

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"sort"
	"strconv"
)

func number(value float64) string { return strconv.FormatFloat(value, 'g', -1, 64) }

func optional(value *float64) string {
	if value == nil {
		return ""
	}
	return number(*value)
}

func writeCSV(path string, header []string, rows [][]string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	w := csv.NewWriter(f)
	if len(rows) > 0 {
		if err := w.Write(header); err != nil {
			f.Close()
			return err
		}
		if err := w.WriteAll(rows); err != nil {
			f.Close()
			return err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func writeReports(out string, runs []RunResult, summaries []Summary, comparisons []Comparison, exclusions map[string]string) error {
	runRows := make([][]string, 0, len(runs))
	for _, r := range runs {
		runRows = append(runRows, []string{r.RunID, strconv.Itoa(r.Round), r.Alg, r.Operation, strconv.Itoa(r.TargetVU),
			strconv.Itoa(r.SuccessfulInWindow), strconv.Itoa(r.SuccessfulStartedInWindow), number(r.ThroughputRPS),
			optional(r.MeanMS), optional(r.P99MS), strconv.Itoa(r.PayloadBytes), strconv.Itoa(r.JWTBytes)})
	}
	if err := writeCSV(filepath.Join(out, "per_run.csv"), []string{"run_id", "round", "alg", "operation", "target_vu", "successful_in_window", "successful_started_in_window", "throughput_rps", "mean_ms", "p99_ms", "payload_bytes", "jwt_bytes"}, runRows); err != nil {
		return err
	}
	summaryHeader := []string{"operation", "target_vu", "alg", "runs", "payload_bytes", "jwt_bytes"}
	for _, metric := range Metrics {
		summaryHeader = append(summaryHeader, metric+"_n", metric+"_mean", metric+"_sd", metric+"_cv")
	}
	summaryRows := make([][]string, 0, len(summaries))
	for _, s := range summaries {
		row := []string{s.Operation, strconv.Itoa(s.TargetVU), s.Alg, strconv.Itoa(s.Runs), strconv.Itoa(s.PayloadBytes), strconv.Itoa(s.JWTBytes)}
		for _, metric := range Metrics {
			stats := s.Stats[metric]
			row = append(row, strconv.Itoa(stats.N), optional(stats.Mean), optional(stats.SD), optional(stats.CV))
		}
		summaryRows = append(summaryRows, row)
	}
	if err := writeCSV(filepath.Join(out, "summary.csv"), summaryHeader, summaryRows); err != nil {
		return err
	}
	comparisonHeader := []string{"operation", "target_vu", "mldsa", "ecdsa"}
	for _, metric := range Metrics {
		comparisonHeader = append(comparisonHeader, metric+"_mldsa", metric+"_ecdsa", metric+"_difference_pct")
	}
	comparisonRows := make([][]string, 0, len(comparisons))
	for _, c := range comparisons {
		row := []string{c.Operation, strconv.Itoa(c.TargetVU), c.MLDSA, c.ECDSA}
		for _, metric := range Metrics {
			values := c.Values[metric]
			row = append(row, optional(values[0]), optional(values[1]), optional(values[2]))
		}
		comparisonRows = append(comparisonRows, row)
	}
	if err := writeCSV(filepath.Join(out, "comparison.csv"), comparisonHeader, comparisonRows); err != nil {
		return err
	}
	ids := make([]string, 0, len(exclusions))
	for id := range exclusions {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	exclusionRows := make([][]string, 0, len(ids))
	for _, id := range ids {
		exclusionRows = append(exclusionRows, []string{id, exclusions[id]})
	}
	if err := writeCSV(filepath.Join(out, "exclusions.csv"), []string{"run_id", "reason"}, exclusionRows); err != nil {
		return err
	}
	return nil
}
