package util

import "testing"

func TestRevHash(t *testing.T) {
	tests := []struct {
		name string
		hash string
		want string
	}{
		{
			"ok",
			"f82aadcc5f683978c0cf7b616b41f956bb6c4bbd073e93ccffb868972e000000",
			"0000002e9768b8ffcc933e07bd4b6cbb56f9416b617bcfc07839685fccad2af8",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RevHash(tt.hash); got != tt.want {
				t.Errorf("RevHash() = %v, want %v", got, tt.want)
			}
		})
	}
}
