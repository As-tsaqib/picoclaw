package commands

import (
	"strconv"
	"strings"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
)

func tableContent(title string, columns []string, rows [][]string, fallback string) bus.StructuredContent {
	return bus.StructuredContent{
		Kind:  "table",
		Title: title,
		Tables: []bus.StructuredTable{{
			Columns: append([]string(nil), columns...),
			Rows:    rows,
			Border:  true,
			Striped: true,
			Header:  true,
		}},
		Fallback: fallback,
	}
}

func keyValueContent(title, text string) bus.StructuredContent {
	rows := make([][]string, 0)
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok || strings.TrimSpace(key) == "" {
			rows = append(rows, []string{"Info", line})
			continue
		}
		rows = append(rows, []string{strings.TrimSpace(key), strings.TrimSpace(value)})
	}
	return tableContent(title, []string{"Properti", "Nilai"}, rows, text)
}

func numberedListContent(title, kind string, values []string, fallback string) bus.StructuredContent {
	rows := make([][]string, 0, len(values))
	for i, value := range values {
		rows = append(rows, []string{strconv.Itoa(i + 1), kind, value, "Aktif"})
	}
	return tableContent(title, []string{"No", "Jenis", "Nama", "Status"}, rows, fallback)
}

func informationalLinesContent(title, text string) bus.StructuredContent {
	rows := make([][]string, 0)
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		line = strings.TrimSpace(strings.TrimPrefix(line, "-"))
		rows = append(rows, []string{strconv.Itoa(len(rows) + 1), line})
	}
	return tableContent(title, []string{"No", "Informasi"}, rows, text)
}
