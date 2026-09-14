package queue

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWorkqueue_NormalItemsCoalesce(t *testing.T) {
	// Normal queue items with the same Key/GVK are coalesced.
	q := NewWorkqueue("test")

	item := QueueItem{
		Key: "default/app",
		GVK: "apps/v1",
	}

	q.Add(item)
	q.Add(item)
	q.Add(item)

	assert.Equal(t, 1, q.Depth())
}

func TestWorkqueue_EventAwareItemsDoNotCoalesce(t *testing.T) {
	// Event-aware items remain distinct even when they target the same resource.
	q := NewWorkqueue("test")

	s1 := map[string]string{
		"generationChanged": "true",
	}
	s2 := map[string]string{
		"labelsChanged": "true",
	}

	obj, _ := objWithGVK()

	q.EnqueueWithEventSentinels(obj, "apps/v1", s1)
	q.EnqueueWithEventSentinels(obj, "apps/v1", s2)

	assert.Equal(t, 2, q.Depth())

	item1, _ := q.Get()
	item2, _ := q.Get()

	assert.NotEqual(t, item1.EventID, item2.EventID)
	assert.Equal(t, s1, q.Sentinels(item1))
	assert.Equal(t, s2, q.Sentinels(item2))
}

func TestWorkqueue_SentinelItemsCoalesce(t *testing.T) {
	// Sentinel-bearing items still use normal queue deduplication when not event-aware.
	q := NewWorkqueue("test")

	obj, _ := objWithGVK()

	s1 := map[string]string{
		"generationChanged": "true",
	}
	s2 := map[string]string{
		"labelsChanged": "true",
	}

	q.EnqueueWithSentinels(obj, "apps/v1", s1)
	q.EnqueueWithSentinels(obj, "apps/v1", s2)

	assert.Equal(t, 1, q.Depth())

	item, shutdown := q.Get()
	assert.False(t, shutdown)

	assert.Equal(t, uint64(0), item.EventID)
	assert.Equal(t, s2, q.Sentinels(item))
}

func TestWorkqueue_SentinelsReturnsCopy(t *testing.T) {
	// Reading sentinel state must not expose the queue's internal mutable map.
	q := NewWorkqueue("test")

	obj, _ := objWithGVK()

	sentinels := map[string]string{
		"generationChanged": "true",
	}

	q.EnqueueWithEventSentinels(obj, "apps/v1", sentinels)

	item, shutdown := q.Get()
	assert.False(t, shutdown)

	got := q.Sentinels(item)
	got["generationChanged"] = "false"
	got["labelsChanged"] = "true"

	assert.Equal(t, "true", q.Sentinels(item)["generationChanged"])
	assert.NotContains(t, q.Sentinels(item), "labelsChanged")
}

func TestWorkqueue_ForgetRemovesSentinels(t *testing.T) {
	// Forgetting an item must release its associated sentinel payload.
	q := NewWorkqueue("test")

	obj, _ := objWithGVK()

	sentinels := map[string]string{
		"generationChanged": "true",
	}

	q.EnqueueWithEventSentinels(obj, "apps/v1", sentinels)

	item, shutdown := q.Get()
	assert.False(t, shutdown)

	assert.Equal(t, sentinels, q.Sentinels(item))

	q.Forget(item)

	assert.Empty(t, q.Sentinels(item))
}

func TestWorkqueue_EventAwareEventsKeepIndependentSentinels(t *testing.T) {
	// Each preserved event must retain only the sentinel state computed for that event.
	q := NewWorkqueue("test")

	obj, _ := objWithGVK()

	first := map[string]string{
		"generationChanged": "true",
		"labelsChanged":     "false",
	}

	second := map[string]string{
		"generationChanged": "false",
		"labelsChanged":     "true",
	}

	q.EnqueueWithEventSentinels(obj, "apps/v1", first)
	q.EnqueueWithEventSentinels(obj, "apps/v1", second)

	item1, shutdown := q.Get()
	assert.False(t, shutdown)

	item2, shutdown := q.Get()
	assert.False(t, shutdown)

	assert.Equal(t, first, q.Sentinels(item1))
	assert.Equal(t, second, q.Sentinels(item2))
	assert.NotEqual(t, item1.EventID, item2.EventID)
	assert.NotEqual(t, q.Sentinels(item1), q.Sentinels(item2))
}

func TestWorkqueue_DoneDoesNotRemoveSentinels(t *testing.T) {
	// Done must retain sentinel state so the item can be requeued before Forget.
	q := NewWorkqueue("test")

	obj, _ := objWithGVK()

	sentinels := map[string]string{
		"generationChanged": "true",
	}

	q.EnqueueWithEventSentinels(obj, "apps/v1", sentinels)

	item, shutdown := q.Get()
	assert.False(t, shutdown)

	q.Done(item)

	assert.Equal(t, sentinels, q.Sentinels(item))

	q.Forget(item)

	assert.Empty(t, q.Sentinels(item))
}

func TestWorkqueue_EventAwareItemsHaveUniqueEventIDs(t *testing.T) {
	// Every event-aware enqueue must receive a unique non-zero event identity.
	q := NewWorkqueue("test")

	obj, _ := objWithGVK()

	sentinels := map[string]string{
		"generationChanged": "true",
	}

	q.EnqueueWithEventSentinels(obj, "apps/v1", sentinels)
	q.EnqueueWithEventSentinels(obj, "apps/v1", sentinels)
	q.EnqueueWithEventSentinels(obj, "apps/v1", sentinels)

	item1, _ := q.Get()
	item2, _ := q.Get()
	item3, _ := q.Get()

	assert.NotZero(t, item1.EventID)
	assert.NotZero(t, item2.EventID)
	assert.NotZero(t, item3.EventID)

	assert.NotEqual(t, item1.EventID, item2.EventID)
	assert.NotEqual(t, item1.EventID, item3.EventID)
	assert.NotEqual(t, item2.EventID, item3.EventID)
}
