package jav

import (
	"reflect"
	"testing"
)

func TestParseActressName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		primary string
		aliases []string
	}{
		{name: "full width alias", input: "和葉みれい（藤白まき）", primary: "和葉みれい", aliases: []string{"藤白まき"}},
		{name: "ascii aliases", input: "A (B, C)", primary: "A", aliases: []string{"B", "C"}},
		{name: "multiple groups", input: "A（B）(C)", primary: "A", aliases: []string{"B", "C"}},
		{name: "empty primary", input: "（藤白まき）", primary: "藤白まき"},
		{name: "plain", input: "  蒲田みらい  ", primary: "蒲田みらい"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseActressName(tt.input)
			if got.Primary != tt.primary || !reflect.DeepEqual(got.Aliases, tt.aliases) {
				t.Fatalf("ParseActressName(%q) = %#v, want primary=%q aliases=%#v", tt.input, got, tt.primary, tt.aliases)
			}
		})
	}
}

func TestIsJapaneseName(t *testing.T) {
	if !IsJapaneseName("小橋りえこ") {
		t.Fatal("expected Japanese-script name")
	}
	if IsJapaneseName("Rieko Kobashi") {
		t.Fatal("roman name must not be classified as Japanese")
	}
}
