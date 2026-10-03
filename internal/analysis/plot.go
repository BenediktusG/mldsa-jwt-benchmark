package analysis

import (
	"fmt"
	"html"
	"math"
	"os"
	"strings"

	"mldsa-jwt-benchmark/internal/profile"
)

var colors = []string{"#2452a2", "#f18828", "#53994c", "#823c9a", "#c94646", "#168d94"}

func MakeSVG(path string, summaries []Summary, operation, metric string) error {
	records := make(map[string]Summary)
	for _, summary := range summaries {
		if summary.Operation == operation {
			records[fmt.Sprintf("%s/%d", summary.Alg, summary.TargetVU)] = summary
		}
	}
	if len(records) == 0 {
		return nil
	}
	ymax := 0.0
	hasValue := false
	for _, summary := range records {
		stats := summary.Stats[metric]
		if stats.Mean == nil {
			continue
		}
		hasValue = true
		value := *stats.Mean
		if stats.SD != nil {
			value += *stats.SD
		}
		if value > ymax {
			ymax = value
		}
	}
	if !hasValue {
		ymax = 1
	} else {
		ymax *= 1.12
		if ymax == 0 {
			ymax = 1
		}
	}
	xAt := func(vu int) float64 { return 85 + math.Log10(float64(vu))/3*650 }
	yAt := func(value float64) float64 { return 485 - value/ymax*405 }
	var b strings.Builder
	fmt.Fprintln(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="850" height="570" viewBox="0 0 850 570">`)
	fmt.Fprintln(&b, `<rect width="850" height="570" fill="white"/>`)
	fmt.Fprintf(&b, `<text x="85" y="30" font-size="20" font-family="sans-serif">%s: %s</text>`+"\n", html.EscapeString(operation), html.EscapeString(metric))
	fmt.Fprintln(&b, `<path d="M85 80 V485 H750" fill="none" stroke="#333"/>`)
	for i := 0; i < 5; i++ {
		value := ymax * float64(i) / 4
		y := yAt(value)
		fmt.Fprintf(&b, `<path d="M85 %.1f H750" stroke="#ddd"/>`+"\n", y)
		fmt.Fprintf(&b, `<text x="8" y="%.1f" font-size="12" font-family="sans-serif">%.2f</text>`+"\n", y+4, value)
	}
	for _, vu := range []int{1, 10, 100, 1000} {
		fmt.Fprintf(&b, `<text x="%.1f" y="510" font-size="13" font-family="sans-serif">%d</text>`+"\n", xAt(vu)-12, vu)
	}
	fmt.Fprintln(&b, `<text x="390" y="545" font-size="14" font-family="sans-serif">Virtual users (log scale)</text>`)
	for index, alg := range profile.Algorithms {
		color := colors[index]
		points := make([]string, 0, 4)
		for _, vu := range []int{1, 10, 100, 1000} {
			summary, ok := records[fmt.Sprintf("%s/%d", alg, vu)]
			if !ok {
				continue
			}
			stats := summary.Stats[metric]
			if stats.Mean == nil {
				continue
			}
			x, y := xAt(vu), yAt(*stats.Mean)
			points = append(points, fmt.Sprintf("%.1f,%.1f", x, y))
			if stats.SD != nil {
				y1, y2 := yAt(*stats.Mean-*stats.SD), yAt(*stats.Mean+*stats.SD)
				fmt.Fprintf(&b, `<path d="M%.1f %.1f V%.1f M%.1f %.1f H%.1f M%.1f %.1f H%.1f" stroke="%s"/>`+"\n", x, y1, y2, x-4, y1, x+4, x-4, y2, x+4, color)
			}
			fmt.Fprintf(&b, `<circle cx="%.1f" cy="%.1f" r="3" fill="%s"/>`+"\n", x, y, color)
		}
		if len(points) > 1 {
			fmt.Fprintf(&b, `<polyline points="%s" fill="none" stroke="%s" stroke-width="2"/>`+"\n", strings.Join(points, " "), color)
		}
		fmt.Fprintf(&b, `<text x="%d" y="%d" font-size="12" fill="%s" font-family="sans-serif">%s</text>`+"\n", 570+(index%2)*140, 35+(index/2)*16, color, html.EscapeString(alg))
	}
	fmt.Fprintln(&b, `</svg>`)
	return os.WriteFile(path, []byte(b.String()), 0644)
}
