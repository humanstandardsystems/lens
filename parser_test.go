package main

import "testing"

func TestParseResetInput(t *testing.T) {
	cases := []struct {
		in      string
		wantDay string
		wantHr  int
		ok      bool
	}{
		// Full day, various time formats
		{"friday 6pm", "friday", 18, true},
		{"friday 6:00pm", "friday", 18, true},
		{"friday 6 pm", "friday", 18, true},
		{"friday 6:00 pm", "friday", 18, true},
		{"FRIDAY 6PM", "friday", 18, true},
		{"Friday 6:00 PM", "friday", 18, true},
		{"friday 18", "friday", 18, true},
		{"friday 18:00", "friday", 18, true},
		// Day order swapped
		{"6pm friday", "friday", 18, true},
		{"18:00 tuesday", "tuesday", 18, true},
		// Generous abbreviations Source specified
		{"m 9am", "monday", 9, true},
		{"mo 9am", "monday", 9, true},
		{"mon 9am", "monday", 9, true},
		{"tu 9am", "tuesday", 9, true},
		{"tue 9am", "tuesday", 9, true},
		{"tues 9am", "tuesday", 9, true},
		{"w 9am", "wednesday", 9, true},
		{"we 9am", "wednesday", 9, true},
		{"wed 9am", "wednesday", 9, true},
		{"th 9am", "thursday", 9, true},
		{"thu 9am", "thursday", 9, true},
		{"thur 9am", "thursday", 9, true},
		{"thurs 9am", "thursday", 9, true},
		{"f 9am", "friday", 9, true},
		{"fr 9am", "friday", 9, true},
		{"fri 9am", "friday", 9, true},
		{"sa 9am", "saturday", 9, true},
		{"sat 9am", "saturday", 9, true},
		{"su 9am", "sunday", 9, true},
		{"sun 9am", "sunday", 9, true},
		// Midnight/noon edge cases
		{"sunday 12am", "sunday", 0, true},
		{"sunday 12pm", "sunday", 12, true},
		{"monday 0", "monday", 0, true},
		{"monday 23", "monday", 23, true},
		// Rejections
		{"", "", 0, false},
		{"friday", "", 0, false},
		{"6pm", "", 0, false},
		{"notaday 6pm", "", 0, false},
		{"friday 25", "", 0, false},
		{"friday 13pm", "", 0, false}, // 13pm is nonsensical, rejected
		{"t 9am", "", 0, false},       // t is ambiguous
		{"s 9am", "", 0, false},       // s is ambiguous
	}

	for _, c := range cases {
		gotDay, gotHr, gotOk := parseResetInput(c.in)
		if gotOk != c.ok || gotDay != c.wantDay || gotHr != c.wantHr {
			t.Errorf("parseResetInput(%q) = (%q, %d, %v); want (%q, %d, %v)",
				c.in, gotDay, gotHr, gotOk, c.wantDay, c.wantHr, c.ok)
		}
	}
}
