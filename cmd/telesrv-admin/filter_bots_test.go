package main

import "testing"

func TestFilterOutBot(t *testing.T) {
	rows := []BotRow{{ID: 1}, {ID: 2}, {ID: 3}}
	out := filterOutBot(rows, 2)
	if len(out) != 2 || out[0].ID != 1 || out[1].ID != 3 {
		t.Fatalf("filterOutBot = %+v, want [1, 3]", out)
	}
}

func TestFilterOutBotNoMatch(t *testing.T) {
	rows := []BotRow{{ID: 1}, {ID: 3}}
	out := filterOutBot(rows, 2)
	if len(out) != 2 || out[0].ID != 1 || out[1].ID != 3 {
		t.Fatalf("filterOutBot (no match) = %+v, want unchanged [1, 3]", out)
	}
}
