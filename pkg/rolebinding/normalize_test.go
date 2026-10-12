package rolebinding

import "testing"

func TestNormalizeEmail(t *testing.T) {
	for _, tc := range []struct {
		input, want string
		invalid     bool
	}{
		{" Alice@EXAMPLE.COM ", "Alice@example.com", false},
		{"A\u0301lice@E\u0301XAMPLE.COM", "Álice@éxample.com", false},
		{"Álice@ÉXAMPLE.COM", "Álice@éxample.com", false},
		{"", "", true}, {"   ", "", true}, {"no-at", "", true}, {"a@@b", "", true}, {"@domain", "", true}, {"local@", "", true},
	} {
		t.Run(tc.input, func(t *testing.T) {
			got, err := NormalizeEmail(tc.input)
			if (err != nil) != tc.invalid || got != tc.want {
				t.Fatalf("got %q, %v; want %q invalid=%v", got, err, tc.want, tc.invalid)
			}
		})
	}
}
