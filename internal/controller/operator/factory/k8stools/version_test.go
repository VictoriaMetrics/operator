package k8stools

import "testing"

func TestIsPSPSupported(t *testing.T) {
	tests := []struct {
		name  string
		want  bool
		major uint64
		minor uint64
	}{
		{
			name:  "yes",
			major: 1,
			minor: 22,
			want:  true,
		},
		{
			name:  "no",
			major: 1,
			minor: 25,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ServerMinorVersion = tt.minor
			ServerMajorVersion = tt.major
			if got := IsPSPSupported(); got != tt.want {
				t.Errorf("IsPSPSupported() = %v, want %v", got, tt.want)
			}
		})
	}
}
