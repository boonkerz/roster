package flow

import (
	"fmt"
	"strings"
)

// unifiedDiff erzeugt einen kompakten Unified-Diff (3 Zeilen Kontext). Für Dateien bis
// 4000 Zeilen per LCS, darüber nur die geänderten Zeilen bei gleicher Zeilenzahl.
func unifiedDiff(name, a, b string) string {
	al, bl := strings.Split(a, "\n"), strings.Split(b, "\n")
	ops := diffOps(al, bl)
	if ops == nil {
		return fmt.Sprintf("--- %s\n+++ %s\n(Datei zu groß für eine Vorschau)\n", name, name)
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "--- %s\n+++ %s\n", name, name)
	const ctx = 3
	// Hunks: Bereiche um Änderungen.
	n := len(ops)
	i := 0
	for i < n {
		for i < n && ops[i].k == ' ' {
			i++
		}
		if i >= n {
			break
		}
		start := i - ctx
		if start < 0 {
			start = 0
		}
		end := i
		for end < n {
			if ops[end].k != ' ' {
				end++
				continue
			}
			// so lange Kontext, bis mehr als 2*ctx unveränderte Zeilen folgen
			j := end
			for j < n && ops[j].k == ' ' && j-end < 2*ctx+1 {
				j++
			}
			if j < n && ops[j].k != ' ' && j-end <= 2*ctx {
				end = j
				continue
			}
			break
		}
		stop := end + ctx
		if stop > n {
			stop = n
		}
		fmt.Fprintf(&sb, "@@ -%d +%d @@\n", ops[start].a+1, ops[start].b+1)
		for _, o := range ops[start:stop] {
			sb.WriteByte(o.k)
			sb.WriteString(o.s)
			sb.WriteByte('\n')
		}
		i = stop
	}
	return sb.String()
}

type op struct {
	k    byte // ' ', '-', '+'
	s    string
	a, b int // Zeilenindex in a bzw. b
}

func diffOps(a, b []string) []op {
	if len(a) == len(b) && len(a) > 4000 {
		var ops []op
		for i := range a {
			if a[i] == b[i] {
				ops = append(ops, op{' ', a[i], i, i})
			} else {
				ops = append(ops, op{'-', a[i], i, i}, op{'+', b[i], i, i})
			}
		}
		return ops
	}
	if len(a) > 4000 || len(b) > 4000 {
		return nil
	}
	// LCS-Tabelle
	m, n := len(a), len(b)
	dp := make([][]int32, m+1)
	for i := range dp {
		dp[i] = make([]int32, n+1)
	}
	for i := m - 1; i >= 0; i-- {
		for j := n - 1; j >= 0; j-- {
			if a[i] == b[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}
	var ops []op
	i, j := 0, 0
	for i < m && j < n {
		switch {
		case a[i] == b[j]:
			ops = append(ops, op{' ', a[i], i, j})
			i++
			j++
		case dp[i+1][j] >= dp[i][j+1]:
			ops = append(ops, op{'-', a[i], i, j})
			i++
		default:
			ops = append(ops, op{'+', b[j], i, j})
			j++
		}
	}
	for ; i < m; i++ {
		ops = append(ops, op{'-', a[i], i, j})
	}
	for ; j < n; j++ {
		ops = append(ops, op{'+', b[j], i, j})
	}
	return ops
}
