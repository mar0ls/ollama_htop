package store

import "time"

const (
	ringSlots = 60 // number of time slots
	slotSecs  = 5  // seconds per slot → 60 × 5 s = 5-min window
)

// ring is a fixed-size circular buffer of averaged float64 values keyed by time.
// Each slot covers slotSecs seconds. Stale slots are reset automatically when a
// new epoch writes to the same position, so the buffer never needs compaction.
type ring struct {
	sums   [ringSlots]float64
	counts [ringSlots]int
	epochs [ringSlots]int64 // unix_time / slotSecs for each slot
}

func (r *ring) record(v float64, t time.Time) {
	epoch := t.Unix() / slotSecs
	slot := int(epoch % ringSlots)
	if r.epochs[slot] != epoch {
		r.sums[slot] = 0
		r.counts[slot] = 0
		r.epochs[slot] = epoch
	}
	r.sums[slot] += v
	r.counts[slot]++
}

// history returns ringSlots averages ordered oldest → newest.
// Slots that have not been written in the current window return 0.
func (r *ring) history(now time.Time) []float64 {
	cur := now.Unix() / slotSecs
	out := make([]float64, ringSlots)
	for i := 0; i < ringSlots; i++ {
		epoch := cur - int64(ringSlots-1-i)
		slot := int(epoch % ringSlots)
		if slot < 0 {
			slot += ringSlots
		}
		if r.epochs[slot] == epoch && r.counts[slot] > 0 {
			out[i] = r.sums[slot] / float64(r.counts[slot])
		}
	}
	return out
}

// peak returns the highest per-slot average in the current window.
func (r *ring) peak(now time.Time) float64 {
	cur := now.Unix() / slotSecs
	var max float64
	for i := 0; i < ringSlots; i++ {
		epoch := cur - int64(i)
		slot := int(epoch % ringSlots)
		if slot < 0 {
			slot += ringSlots
		}
		if r.epochs[slot] == epoch && r.counts[slot] > 0 {
			v := r.sums[slot] / float64(r.counts[slot])
			if v > max {
				max = v
			}
		}
	}
	return max
}

// windowStart returns the timestamp of the oldest slot that could have data,
// clamped to appStart so newly started instances show a short window.
func (r *ring) windowStart(appStart, now time.Time) time.Time {
	limit := now.Add(-time.Duration(ringSlots*slotSecs) * time.Second)
	if appStart.After(limit) {
		return appStart
	}
	return limit
}

// activeBuckets returns how many slots have been reachable since appStart.
func activeBuckets(appStart, now time.Time) int {
	elapsed := now.Sub(appStart)
	n := int(elapsed.Seconds()/slotSecs) + 1
	if n > ringSlots {
		n = ringSlots
	}
	if n < 1 {
		n = 1
	}
	return n
}
