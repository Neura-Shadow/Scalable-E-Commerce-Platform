package utils

import (
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestHashAndSalt(t *testing.T) {
	type args struct {
		pass []byte
	}
	tests := []struct {
		name  string
		args  args
		empty bool
	}{
		{
			name:  "hash successfully",
			args:  args{pass: []byte("test")},
			empty: false,
		},
		{
			name:  "hash too long",
			args:  args{pass: []byte("01234567890123456789012345678901234567890123456789012345678901234567890123456789")},
			empty: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := HashAndSalt(tt.args.pass)
			if tt.empty && err == nil {
				t.Fatal("expected hashing error")
			}
			if !tt.empty && err != nil {
				t.Fatalf("unexpected hashing error: %v", err)
			}
			if (got == "") != tt.empty {
				t.Errorf("HashAndSalt() = %v, args %v", got, tt.args)
			}
			if got != "" {
				cost, err := bcrypt.Cost([]byte(got))
				if err != nil {
					t.Fatalf("cannot read bcrypt cost: %v", err)
				}
				if cost != bcrypt.DefaultCost {
					t.Fatalf("bcrypt cost = %d, want %d", cost, bcrypt.DefaultCost)
				}
			}
		})
	}
}
