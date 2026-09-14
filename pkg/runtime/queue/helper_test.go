// pkg/queue/helper_test.go
package queue

import (
	"testing"

	"github.com/orkspace/orkestra/domain"
	"github.com/stretchr/testify/assert"
)

func objWithGVK() (obj interface{}, gvk string) {
	return domain.UnstructuredForTest(), domain.GVKForTest()
}

func queueItemWithGVK() QueueItem {
	return QueueItemForTest(domain.KeyForTest(), domain.GVKForTest())
}

func TestResolveKeyFromCache(t *testing.T) {
	obj, gvk := objWithGVK()
	key := resolveKeyFromCache(obj, gvk)
	assert.Equal(t, "default/example", key)
}

func TestAddWithEvalNilQueue(t *testing.T) {
	q := NewWorkqueueForTest(queueConfigForTest{})
	q.q.addWithEval(queueItemWithGVK())
	assert.Equal(t, 1, q.q.queue.Len())
}

func TestAddWithEvalUnlimitedQueue(t *testing.T) {
	q := NewWorkqueueForTest(queueConfigForTest{maxDepth: 0})
	q.q.addWithEval(queueItemWithGVK())
	assert.Equal(t, 1, q.q.queue.Len())
}

func TestEvaluateQueueBehaviour(t *testing.T) {

}

func TestQueueInfo(t *testing.T) {

}

func TestBehaviourCond(t *testing.T) {

}

func TestOnLimitCond(t *testing.T) {

}

func TestOnThresholdCond(t *testing.T) {

}

func TestNeedsBehaviourEval(t *testing.T) {

}

func TestMaxDepth(t *testing.T) {

}

func TestIsUnlimited(t *testing.T) {

}

func TestDepthReached(t *testing.T) {

}
