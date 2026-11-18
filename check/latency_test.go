package check

import (
	"testing"
	"time"
)

func TestLatencyWithinLimit(t *testing.T) {
	tests := []struct {
		name string
		d    time.Duration
		max  int
		want bool
	}{
		{name: "阈值以内", d: 299 * time.Millisecond, max: 300, want: true},
		{name: "恰好达到阈值", d: 300 * time.Millisecond, max: 300, want: false},
		{name: "超过阈值一纳秒", d: 300*time.Millisecond + time.Nanosecond, max: 300, want: false},
		{name: "请求超时", d: 5 * time.Second, max: 300, want: false},
		{name: "未设置阈值", d: 10 * time.Millisecond, max: 0, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := latencyWithinLimit(tt.d, tt.max); got != tt.want {
				t.Fatalf("latencyWithinLimit(%v, %d) = %v, want %v", tt.d, tt.max, got, tt.want)
			}
		})
	}
}
