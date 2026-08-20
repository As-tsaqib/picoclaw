package commands

import (
	"strconv"

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

func detailHeaderColumns() []string {
	return bus.CardHeaderColumns(bus.CardHeaderDetail, true)
}

func statusHeaderColumns() []string {
	return bus.CardHeaderColumns(bus.CardHeaderStatus, true)
}

func inventoryHeaderColumns() []string {
	return bus.CardHeaderColumns(bus.CardHeaderInventory, true)
}

func numberedListContent(title, kind string, values []string, fallback string) bus.StructuredContent {
	rows := make([][]string, 0, len(values))
	for i, value := range values {
		rows = append(rows, []string{strconv.Itoa(i+1) + ". " + kind + " " + value, "🟢 ᴀᴄᴛɪᴠᴇ"})
	}
	return tableContent(title, inventoryHeaderColumns(), rows, fallback)
}
