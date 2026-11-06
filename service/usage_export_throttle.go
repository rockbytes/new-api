package service

import (
	"context"
	"io"
	"time"
)

// usageThrottleChunk bounds how much a single write waits on the budget.
const usageThrottleChunk = 32 * 1024

// usageThrottle paces writes so one stream averages at most rate bytes per second. The budget
// refills continuously and holds one second of burst. Each download has its own throttle, so the
// limit applies per download.
type usageThrottle struct {
	ctx    context.Context
	w      io.Writer
	rate   float64
	tokens float64
	last   time.Time
	now    func() time.Time
	sleep  func(ctx context.Context, d time.Duration) error
}

// UsageDownloadWriter wraps w so a download streams at the configured bandwidth.
func UsageDownloadWriter(ctx context.Context, w io.Writer) io.Writer {
	rate := float64(usageExportLimits().DownloadBytesPerSecond)
	return newUsageThrottle(ctx, w, rate, time.Now, sleepUsageThrottle)
}

func newUsageThrottle(ctx context.Context, w io.Writer, rate float64, now func() time.Time, sleep func(context.Context, time.Duration) error) *usageThrottle {
	return &usageThrottle{ctx: ctx, w: w, rate: rate, tokens: rate, last: now(), now: now, sleep: sleep}
}

func sleepUsageThrottle(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (t *usageThrottle) Write(p []byte) (int, error) {
	written := 0
	for written < len(p) {
		end := min(written+usageThrottleChunk, len(p))
		if err := t.wait(end - written); err != nil {
			return written, err
		}
		n, err := t.w.Write(p[written:end])
		written += n
		if err != nil {
			return written, err
		}
	}
	return written, nil
}

// wait spends n bytes of budget. When the budget goes negative, it sleeps until the debt is repaid.
func (t *usageThrottle) wait(n int) error {
	now := t.now()
	t.tokens = min(t.rate, t.tokens+now.Sub(t.last).Seconds()*t.rate)
	t.last = now
	t.tokens -= float64(n)
	if t.tokens >= 0 {
		return nil
	}
	delay := time.Duration(-t.tokens / t.rate * float64(time.Second))
	return t.sleep(t.ctx, delay)
}
