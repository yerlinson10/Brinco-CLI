package chat

import "testing"

func TestFindClientInSetExact(t *testing.T) {
	a := &serverClient{name: "Alice"}
	b := &serverClient{name: "Bob"}
	clients := map[*serverClient]struct{}{a: {}, b: {}}

	got, amb := findClientInSet(clients, "Bob")
	if amb || got != b {
		t.Fatalf("got=%v amb=%v want Bob", got, amb)
	}
}

func TestFindClientInSetFold(t *testing.T) {
	a := &serverClient{name: "Alice"}
	clients := map[*serverClient]struct{}{a: {}}

	got, amb := findClientInSet(clients, "alice")
	if amb || got != a {
		t.Fatalf("got=%v amb=%v want Alice", got, amb)
	}
}

func TestFindClientInSetAmbiguous(t *testing.T) {
	a := &serverClient{name: "bob"}
	b := &serverClient{name: "Bob"}
	clients := map[*serverClient]struct{}{a: {}, b: {}}

	got, amb := findClientInSet(clients, "BOB")
	if !amb || got != nil {
		t.Fatalf("got=%v amb=%v want ambiguous", got, amb)
	}
}

func TestFindClientInSetEmptyName(t *testing.T) {
	a := &serverClient{name: "x"}
	clients := map[*serverClient]struct{}{a: {}}
	got, amb := findClientInSet(clients, "   ")
	if got != nil || amb {
		t.Fatalf("got=%v amb=%v", got, amb)
	}
}
