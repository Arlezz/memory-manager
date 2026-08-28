package claudedir

import "testing"

// TestMangleMatchesObservedDirectories pins the reverse-engineered scheme
// against directories Claude Code actually created on this machine. If a Claude
// Code update changes the mangling, this test is where it shows up.
func TestMangleMatchesObservedDirectories(t *testing.T) {
	tests := []struct{ path, want string }{
		{`C:\Users\Anton\Documents\projects\memory-manager`, "C--Users-Anton-Documents-projects-memory-manager"},
		{`C:\Users\Anton\Documents\projects\ORBIT-X_core`, "C--Users-Anton-Documents-projects-ORBIT-X-core"},
		{`C:\Users\Anton\Documents\projects\ORBIT-X_data_pipeline`, "C--Users-Anton-Documents-projects-ORBIT-X-data-pipeline"},
		{`C:\Windows\System32`, "C--Windows-System32"},
		{`C:\Users\Anton\Desktop\Prueba Tecnica (MQ)`, "C--Users-Anton-Desktop-Prueba-Tecnica--MQ-"},
		{"/home/anton/repos/core", "-home-anton-repos-core"},
	}
	for _, tc := range tests {
		if got := Mangle(tc.path); got != tc.want {
			t.Errorf("Mangle(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}

// TestMangleIsLossy documents why the path cannot be the memory key: two
// different projects collapse to the same directory name.
func TestMangleIsLossy(t *testing.T) {
	a := Mangle(`C:\repos\nova_core`)
	b := Mangle(`C:\repos\nova-core`)
	if a != b {
		t.Fatalf("expected a collision, got %q and %q", a, b)
	}
}
