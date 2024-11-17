package k8stools

import "testing"

func TestIsEndpointSlicesSupported(t *testing.T) {
	tests := []struct {
		name  string
		want  bool
		major uint64
		minor uint64
	}{
		{
			name:  "yes",
			major: 1,
			minor: 28,
			want:  true,
		},
		{
			name:  "yes",
			major: 1,
			minor: 21,
			want:  true,
		},
		{
			name:  "no",
			major: 1,
			minor: 20,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ServerMinorVersion = tt.minor
			ServerMajorVersion = tt.major
			if got := IsEndpointSliceSupported(); got != tt.want {
				t.Errorf("IsEndpointSliceSupported() = %v, want %v", got, tt.want)
			}
		})
	}
}
