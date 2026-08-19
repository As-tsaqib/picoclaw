package agent

import "testing"

func TestExplicitMemoryCaptureIntent(t *testing.T) {
	tests := []struct {
		text string
		want bool
	}{
		{"Saya lebih suka jawaban singkat.", false},
		{"I prefer concise answers.", false},
		{"ingat bahwa saya lebih suka jawaban singkat", true},
		{"Tolong ingat bahwa format quiz saya harus native.", true},
		{"save this to memory: I prefer English", true},
		{"please remember that I use pnpm", true},
		{"hapus ini dari memori", true},
		{"remove this from memory", true},
		{"perbarui yang kamu ingat tentang preferensi format saya", true},
		{"update what you remember about my response format", true},
	}
	for _, tc := range tests {
		t.Run(tc.text, func(t *testing.T) {
			if got := explicitMemoryCaptureIntent(tc.text); got != tc.want {
				t.Fatalf("explicitMemoryCaptureIntent(%q) = %t, want %t", tc.text, got, tc.want)
			}
		})
	}
}
