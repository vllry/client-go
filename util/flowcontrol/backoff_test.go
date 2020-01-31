/*
Copyright 2015 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package flowcontrol

import (
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/util/clock"
)

func TestSlowBackoff(t *testing.T) {
	id := "_idSlow"
	tc := clock.NewFakeClock(time.Now())
	step := time.Second
	maxDuration := 50 * step

	b := NewFakeBackOff(step, maxDuration, tc)
	cases := []time.Duration{0, 1, 2, 4, 8, 16, 32, 50, 50, 50}
	for ix, c := range cases {
		tc.Step(step)
		wait := b.Get(id)

		// TODO(vllry) account for nested jitter.
		expectBaseBackoff := c * step
		expectMaxBackoffNanoseconds := int64(float64(expectBaseBackoff.Nanoseconds()) * (float64(1) + DefaultJitterRatio))
		expectMaxBackoff := time.Duration(expectMaxBackoffNanoseconds)

		if wait < expectBaseBackoff || wait > expectMaxBackoff {
			t.Errorf("input: '%d': expected between %s and %s, got %s", ix, expectBaseBackoff, expectMaxBackoff, wait)
		}
		b.Next(id, tc.Now())
	}

	//Now confirm that the Reset cancels backoff.
	b.Next(id, tc.Now())
	b.Reset(id)
	if b.Get(id) != 0 {
		t.Errorf("Reset didn't clear the backoff.")
	}

}

func TestBackoffReset(t *testing.T) {
	id := "_idReset"
	tc := clock.NewFakeClock(time.Now())
	step := time.Second
	maxDuration := step * 5
	b := NewFakeBackOff(step, maxDuration, tc)
	startTime := tc.Now()

	// get to backoff = maxDuration
	for i := 0; i <= int(maxDuration/step); i++ {
		tc.Step(step)
		b.Next(id, tc.Now())
	}

	// backoff should be capped at maxDuration
	if !b.IsInBackOffSince(id, tc.Now()) {
		t.Errorf("expected to be in Backoff got %s", b.Get(id))
	}

	lastUpdate := tc.Now()
	tc.Step(2*maxDuration + step) // time += 11s, 11 > 2*maxDuration
	if b.IsInBackOffSince(id, lastUpdate) {
		t.Errorf("expected to not be in Backoff after reset (start=%s, now=%s, lastUpdate=%s), got %s", startTime, tc.Now(), lastUpdate, b.Get(id))
	}
}

func TestBackoffHighWaterMark(t *testing.T) {
	id := "_idHiWaterMark"
	tc := clock.NewFakeClock(time.Now())
	step := time.Second
	maxBaseDuration := 5 * step
	maxTotalNanoseconds := int64(float64(maxBaseDuration.Nanoseconds()) * (float64(1) + DefaultJitterRatio))
	maxTotalDuration := time.Duration(maxTotalNanoseconds)
	b := NewFakeBackOff(step, maxBaseDuration, tc)

	// get to backoff = maxBaseDuration
	for i := 0; i <= int(maxBaseDuration/step); i++ {
		tc.Step(step)
		b.Next(id, tc.Now())
	}

	// backoff high watermark expires after 2*maxBaseDuration
	tc.Step(maxBaseDuration + step)
	b.Next(id, tc.Now())

	currentBackOff := b.Get(id)
	if currentBackOff < maxBaseDuration || currentBackOff > maxTotalDuration {
		t.Errorf("expected Backoff to stay at high watermark %s to %s, got %s", maxBaseDuration, maxTotalDuration, b.Get(id))
	}
}

func TestBackoffGC(t *testing.T) {
	id := "_idGC"
	tc := clock.NewFakeClock(time.Now())
	step := time.Second
	maxDuration := 5 * step

	b := NewFakeBackOff(step, maxDuration, tc)

	for i := 0; i <= int(maxDuration/step); i++ {
		tc.Step(step)
		b.Next(id, tc.Now())
	}
	lastUpdate := tc.Now()
	tc.Step(maxDuration + step)
	b.GC()
	_, found := b.perItemBackoff[id]
	if !found {
		t.Errorf("expected GC to skip entry, elapsed time=%s maxDuration=%s", tc.Since(lastUpdate), maxDuration)
	}

	tc.Step(maxDuration + step)
	b.GC()
	r, found := b.perItemBackoff[id]
	if found {
		t.Errorf("expected GC of entry after %s got entry %v", tc.Since(lastUpdate), r)
	}
}

func TestIsInBackOffSinceUpdate(t *testing.T) {
	id := "_idIsInBackOffSinceUpdate"
	tc := clock.NewFakeClock(time.Now())
	step := time.Second
	maxBaseDuration := 10 * step
	b := NewFakeBackOff(step, maxBaseDuration, tc)
	startTime := tc.Now()

	cases := []struct {
		tick      time.Duration
		inBackOff bool
		value     int
	}{
		{tick: 0, inBackOff: false, value: 0},
		{tick: 1, inBackOff: false, value: 1},
		{tick: 2, inBackOff: true, value: 2},
		{tick: 3, inBackOff: false, value: 2},
		{tick: 4, inBackOff: true, value: 4},
		{tick: 5, inBackOff: true, value: 4},
		{tick: 6, inBackOff: true, value: 4},
		{tick: 7, inBackOff: false, value: 4},
		{tick: 8, inBackOff: true, value: 8},
		{tick: 9, inBackOff: true, value: 8},
		{tick: 10, inBackOff: true, value: 8},
		{tick: 11, inBackOff: true, value: 8},
		{tick: 12, inBackOff: true, value: 8},
		{tick: 13, inBackOff: true, value: 8},
		{tick: 14, inBackOff: true, value: 8},
		{tick: 15, inBackOff: false, value: 8},
		{tick: 16, inBackOff: true, value: 10},
		{tick: 17, inBackOff: true, value: 10},
		{tick: 18, inBackOff: true, value: 10},
		{tick: 19, inBackOff: true, value: 10},
		{tick: 20, inBackOff: true, value: 10},
		{tick: 21, inBackOff: true, value: 10},
		{tick: 22, inBackOff: true, value: 10},
		{tick: 23, inBackOff: true, value: 10},
		{tick: 24, inBackOff: true, value: 10},
		{tick: 25, inBackOff: false, value: 10},
		{tick: 26, inBackOff: true, value: 10},
		{tick: 27, inBackOff: true, value: 10},
		{tick: 28, inBackOff: true, value: 10},
		{tick: 29, inBackOff: true, value: 10},
		{tick: 30, inBackOff: true, value: 10},
		{tick: 31, inBackOff: true, value: 10},
		{tick: 32, inBackOff: true, value: 10},
		{tick: 33, inBackOff: true, value: 10},
		{tick: 34, inBackOff: true, value: 10},
		{tick: 35, inBackOff: false, value: 10},
		{tick: 56, inBackOff: false, value: 0},
		{tick: 57, inBackOff: false, value: 1},
	}

	for _, c := range cases {
		tc.SetTime(startTime.Add(c.tick * step))
		if c.inBackOff != b.IsInBackOffSinceUpdate(id, tc.Now()) {
			t.Errorf("expected IsInBackOffSinceUpdate %v got %v at tick %s", c.inBackOff, b.IsInBackOffSinceUpdate(id, tc.Now()), c.tick*step)
		}

		// TODO(vllry) account for nested jitter.
		expectBaseBackoff := time.Duration(c.value) * step
		maxTotalNanoseconds := int64(float64(expectBaseBackoff.Nanoseconds()) * (float64(1) + DefaultJitterRatio))
		maxTotalDuration := time.Duration(maxTotalNanoseconds)
		wait := b.Get(id)

		if c.inBackOff && (wait < expectBaseBackoff || wait > maxTotalDuration) {
			t.Errorf("expected backoff between %s and %s, got %s at tick %s", expectBaseBackoff, maxTotalDuration, wait, c.tick*step)
		}

		if !c.inBackOff {
			b.Next(id, tc.Now())
		}
	}
}
