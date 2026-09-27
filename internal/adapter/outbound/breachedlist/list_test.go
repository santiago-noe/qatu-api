package breachedlist

import (
	"context"
	"testing"
)

func TestIsBreached(t *testing.T) {
	l := New()
	if err := l.Load(); err != nil {
		t.Fatal(err)
	}
	if l.Size() < 100_000 {
		t.Fatalf("la lista parece incompleta: %d entradas", l.Size())
	}

	tests := []struct {
		password string
		want     bool
	}{
		{"1234567890", true},
		{"qwertyuiop", true},
		{"QwertyUiop", true}, // sin distinguir mayúsculas
		{"password123", true},
		{"tornillo-verde-rotomartillo-9", false},
	}
	for _, tt := range tests {
		t.Run(tt.password, func(t *testing.T) {
			got, err := l.IsBreached(context.Background(), tt.password)
			if err != nil || got != tt.want {
				t.Fatalf("IsBreached(%q) = %v, %v; se esperaba %v", tt.password, got, err, tt.want)
			}
		})
	}
}
