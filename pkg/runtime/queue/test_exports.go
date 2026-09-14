package queue

import (
	"github.com/orkspace/orkestra/domain"
)

// Methods implementation of the Workqueue interface
type workqueueForTest struct {
	name string
	gvk  string
	q    *Workqueue
	*Workqueue
	cfg queueConfigForTest

	// dummy
	dummy string
}

type queueConfigForTest struct {
	name                  string
	gvk                   string
	maxDepth              int
	thresholdValue        int
	onLimit               bool
	onThreshold           bool
	onLimitConditions     bool
	onThresholdConditions bool
}

var _ domain.Workqueue = (*workqueueForTest)(nil)

func NewWorkqueueForTest(cfg queueConfigForTest) *workqueueForTest {
	return &workqueueForTest{
		name: cfg.name,
		gvk:  cfg.gvk,
		q:    NewWorkqueue(cfg.name),
		cfg:  cfg,
	}
}

// implement all methods
func (q *workqueueForTest) IsUnlimited() bool {
	return q != nil && q.cfg.maxDepth == 0
}
func (q *workqueueForTest) HasBehaviour() bool {
	return q != nil && (q.cfg.onLimit || q.cfg.onThreshold)
}
func (q *workqueueForTest) HasOnLimit() bool {
	return q != nil && q.cfg.onLimit
}
func (q *workqueueForTest) HasOnThreshold() bool {
	return q != nil && q.cfg.onThreshold
}
func (q *workqueueForTest) HasBehaviourCondition() bool {
	return q != nil && (q.cfg.onLimitConditions || q.cfg.onThresholdConditions)
}
func (q *workqueueForTest) HasOnLimitConditions() bool {
	return q != nil && q.cfg.onLimitConditions
}
func (q *workqueueForTest) HasOnThresholdConditions() bool {
	return q != nil && q.cfg.onThresholdConditions
}
func (q *workqueueForTest) MaxQueueDepth() int {
	if q == nil {
		return 0
	}
	return q.cfg.maxDepth
}
func (q *workqueueForTest) ThresholdValue() int {
	if q == nil {
		return 0
	}
	return q.cfg.thresholdValue
}
func (q *workqueueForTest) ThresholdReached(depth int) bool {
	if q == nil {
		return false
	}
	return depth >= q.cfg.thresholdValue
}
func (q *workqueueForTest) Queue() domain.Workqueue {
	if q == nil {
		return nil
	}
	return q
}

func (q *workqueueForTest) Type() string {
	if q == nil {
		return ""
	}
	return q.dummy
}
func (q *workqueueForTest) IsRatelimitedType(s string) bool {
	if q == nil {
		return false
	}
	return q.dummy == s
}
func (q *workqueueForTest) IsDelayedType(s string) bool {
	if q == nil {
		return false
	}
	return q.dummy == s
}

func QueueItemForTest(key, gvk string) QueueItem {
	return QueueItem{
		Key: key,
		GVK: gvk,
	}
}
func BehaviourEvalForTest(onLimit, onThreshold bool) *BehaviourEval {
	b := &BehaviourEval{}
	if onLimit {
		b.OnLimit.Store(true)
	}
	if onThreshold {
		b.OnThreshold.Store(true)
	}
	return b
}
