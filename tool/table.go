package main

import (
	"encoding/csv"
	"fmt"
	"strconv"
	"strings"
)

// readTable reads TSV or CSV (decided by the header line) into rows keyed by
// canonical column names. Chinese and English headers are both accepted.
func readTable(raw string, aliases map[string][]string) ([]map[string]string, []string, error) {
	raw = strings.TrimPrefix(raw, "\uFEFF")
	var lines []string
	for _, l := range strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n") {
		if strings.TrimSpace(l) == "" || strings.HasPrefix(strings.TrimSpace(l), "#") {
			continue
		}
		lines = append(lines, l)
	}
	if len(lines) < 2 {
		return nil, nil, fmt.Errorf("至少要有一行表头和一行数据")
	}
	sep := '\t'
	if !strings.Contains(lines[0], "\t") && strings.Contains(lines[0], ",") {
		sep = ','
	}
	r := csv.NewReader(strings.NewReader(strings.Join(lines, "\n")))
	r.Comma = sep
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	recs, err := r.ReadAll()
	if err != nil {
		return nil, nil, err
	}
	lookup := map[string]string{}
	for canon, names := range aliases {
		lookup[strings.ToLower(canon)] = canon
		for _, n := range names {
			lookup[strings.ToLower(n)] = canon
		}
	}
	var cols, unknown []string
	for _, h := range recs[0] {
		h = strings.TrimSpace(strings.ToLower(h))
		if c, ok := lookup[h]; ok {
			cols = append(cols, c)
		} else {
			cols = append(cols, h)
			unknown = append(unknown, h)
		}
	}
	var rows []map[string]string
	for _, rec := range recs[1:] {
		m := map[string]string{}
		for i, v := range rec {
			if i < len(cols) {
				m[cols[i]] = strings.TrimSpace(v)
			}
		}
		rows = append(rows, m)
	}
	return rows, unknown, nil
}

// num parses 1,234 / 1.2万 / 3.4k / 12% / 1.2w.
func num(s string) (float64, bool) {
	s = strings.TrimSpace(strings.ReplaceAll(s, ",", ""))
	if s == "" || s == "-" {
		return 0, false
	}
	mult := 1.0
	switch {
	case strings.HasSuffix(s, "万"), strings.HasSuffix(strings.ToLower(s), "w"):
		mult, s = 10000, strings.TrimRight(strings.TrimSuffix(s, "万"), "wW")
	case strings.HasSuffix(strings.ToLower(s), "k"):
		mult, s = 1000, strings.TrimRight(s, "kK")
	case strings.HasSuffix(s, "%"):
		mult, s = 0.01, strings.TrimSuffix(s, "%")
	case strings.HasSuffix(s, "s"), strings.HasSuffix(s, "秒"):
		s = strings.TrimSuffix(strings.TrimSuffix(s, "s"), "秒")
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0, false
	}
	return v * mult, true
}

func fmtCount(v float64) string {
	switch {
	case v >= 10000:
		return fmt.Sprintf("%.1f万", v/10000)
	default:
		return fmt.Sprintf("%.0f", v)
	}
}

func pct(v float64) string { return fmt.Sprintf("%.1f%%", v*100) }
