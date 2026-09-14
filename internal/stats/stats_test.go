package stats

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewRecorderStartsEmpty(t *testing.T) {
	now := time.Date(2026, 9, 14, 1, 2, 3, 0, time.UTC)
	r := NewRecorder(now)

	snap := r.Snapshot()
	assert.Equal(t, now, snap.StartedAt)
	assert.Zero(t, snap.Total)
	assert.Zero(t, snap.Success)
	assert.Zero(t, snap.BadRequest)
	assert.Zero(t, snap.Undecodable)
}

func TestRecordCountsEachCategoryOnce(t *testing.T) {
	r := NewRecorder(time.Now())

	r.Record(CategorySuccess)
	r.Record(CategoryBadRequest)
	r.Record(CategoryBadRequest)
	r.Record(CategoryUndecodable)

	snap := r.Snapshot()
	assert.Equal(t, int64(1), snap.Success)
	assert.Equal(t, int64(2), snap.BadRequest)
	assert.Equal(t, int64(1), snap.Undecodable)
	assert.Equal(t, int64(4), snap.Total)
}

func TestRecorderStaysConsistentUnderConcurrency(t *testing.T) {
	r := NewRecorder(time.Now())

	const workers = 64
	const recordsEach = 500
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < recordsEach; j++ {
				r.Record(Category(j % 3))
				// Every snapshot, even one taken mid-flight, must be a
				// consistent read: the total is the sum of the categories.
				snap := r.Snapshot()
				if snap.Total != snap.Success+snap.BadRequest+snap.Undecodable {
					t.Errorf("inconsistent snapshot: %+v", snap)
					return
				}
			}
		}()
	}
	wg.Wait()

	snap := r.Snapshot()
	assert.Equal(t, int64(workers*recordsEach), snap.Total,
		"no record may be lost or counted twice")
	assert.Equal(t, snap.Success+snap.BadRequest+snap.Undecodable, snap.Total)
}
