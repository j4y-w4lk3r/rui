package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func fillBarRow(s string, w int, bar lipgloss.Style) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	if w < 1 {
		return s
	}
	cur := lipgloss.Width(s)
	switch {
	case cur == w:
		return s
	case cur > w:
		return truncateRenderedWidth(s, w)
	default:
		return s + bar.Render(strings.Repeat(" ", w-cur))
	}
}

func truncateRenderedWidth(s string, w int) string {
	if w < 1 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	return ansi.Truncate(s, w, "")
}

func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max <= 1 {
		return string(r[:max])
	}
	return string(r[:max-1]) + "…"
}

func centerBoxOnPlainScreen(box string, termW, termH int) string {
	if termW < 1 {
		termW = 1
	}
	if termH < 1 {
		termH = 1
	}
	boxLines := strings.Split(strings.TrimSuffix(box, "\n"), "\n")
	if len(boxLines) == 0 {
		boxLines = []string{""}
	}
	top := max(0, (termH-len(boxLines))/2)

	bgRow := rowPadStyle.Render(strings.Repeat(" ", termW))
	rows := make([]string, termH)
	for y := 0; y < termH; y++ {
		rows[y] = bgRow
	}
	for i, line := range boxLines {
		y := top + i
		if y >= termH {
			break
		}
		rows[y] = centerRowOnBackground(termW, line)
	}
	return strings.Join(rows, "\n")
}

func centerRowOnBackground(fullW int, content string) string {
	content = strings.ReplaceAll(content, "\n", "")
	content = strings.ReplaceAll(content, "\r", "")
	cw := lipgloss.Width(content)
	if cw >= fullW {
		return truncateRenderedWidth(content, fullW)
	}
	left := (fullW - cw) / 2
	right := fullW - cw - left
	return rowPadStyle.Render(strings.Repeat(" ", left)) + content + rowPadStyle.Render(strings.Repeat(" ", right))
}
