package metrictime_test

import (
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/metrictime"
)

func TestMetricTimeTruncatesToMillisecond(t *testing.T) {
	base := time.Date(2024, 1, 15, 10, 30, 45, 0, time.UTC)
	cases := []struct {
		name string
		in   time.Time
		want time.Time
	}{
		{
			name: "already on a millisecond",
			in:   base.Add(500 * time.Millisecond),
			want: base.Add(500 * time.Millisecond),
		},
		{
			name: "sub-millisecond digits stripped",
			in:   base.Add(500*time.Millisecond + 999*time.Microsecond + 999),
			want: base.Add(500 * time.Millisecond),
		},
		{
			name: "zero time unchanged",
			in:   time.Time{},
			want: time.Time{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := metrictime.MetricTime(tc.in)
			if !got.Equal(tc.want) {
				t.Errorf("MetricTime(%v) = %v; want %v", tc.in, got, tc.want)
			}
		})
	}
}
