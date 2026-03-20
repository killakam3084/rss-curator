package main

import (
	"reflect"
	"testing"
)

func TestParseCSV(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{name: "empty", input: "", want: nil},
		{name: "whitespace", input: "   ", want: nil},
		{name: "single", input: "The Pitt", want: []string{"The Pitt"}},
		{name: "multiple", input: "The Pitt,Severance", want: []string{"The Pitt", "Severance"}},
		{name: "trim and skip empties", input: " The Pitt , , Severance ,,", want: []string{"The Pitt", "Severance"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseCSV(tt.input)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("parseCSV(%q) = %#v, want %#v", tt.input, got, tt.want)
			}
		})
	}
}
