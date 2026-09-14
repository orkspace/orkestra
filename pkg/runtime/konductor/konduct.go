package konductor

import (
	"context"
	"os"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/orkspace/orkestra/domain"
	"github.com/orkspace/orkestra/pkg/event"
	"github.com/orkspace/orkestra/pkg/kubeclient"
	"github.com/orkspace/orkestra/pkg/logger"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	"github.com/orkspace/orkestra/pkg/utils"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
)

type KonductorElection struct {
	name       string
	kube       *kubeclient.Kubeclient
	event      *event.Event
	cancelFunc context.CancelFunc
	runCancel  context.CancelFunc
	run        func(context.Context)

	election theElection
	opts     Options
	started  bool
}

type theElection struct {
	startedKonducting atomic.Bool
	konductor         string
	onElected         func(konductor string)
}

type Options struct {
	LeaseDuration time.Duration
	RetryPeriod   time.Duration
	RenewDeadline time.Duration

	Namespace   string
	Labels      map[string]string
	Annotations map[string]string
}

var _ domain.Komponent = (*KonductorElection)(nil)

func NewKonductorElection(
	kube *kubeclient.Kubeclient,
	event *event.Event,
	run func(context.Context),
	onElected func(konductor string),
	opts Options,
) *KonductorElection {
	if opts.Namespace == "" {
		opts.Namespace = "default"
	}

	ko := &KonductorElection{
		name:  orktypes.KonductorLeaseName,
		event: event,
		kube:  kube,
		run:   run,
		opts:  opts,
	}

	ko.election.startedKonducting.Store(false)
	ko.election.onElected = onElected
	ko.election.konductor = ""

	return ko
}

func (ko *KonductorElection) Start(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	// Create a cancellable context for the leader election
	leaderCtx, cancel := context.WithCancel(ctx)
	ko.cancelFunc = cancel

	go func() {
		leaderelection.RunOrDie(leaderCtx, ko.leaseConfig())
	}()

	ko.started = true
	return nil
}

func (ko *KonductorElection) Started() bool { return ko.started }

func (ko *KonductorElection) Shutdown(ctx context.Context) {
	logger.Info().Msg("🛑 Shutting down konductor election...")

	// Cancel the konductor election context
	if ko.cancelFunc != nil {
		ko.cancelFunc()
	}

	// Give it a moment to release the lease
	utils.Sleep(2)
	logger.Info().Msgf("%s Konductor election shut down", utils.SuccessMark())
}

func (ko *KonductorElection) Name() string {
	return ko.name
}

func (ko *KonductorElection) kind() string {
	return "Lease"
}

// Helpers
// Lease configuration
func (ko *KonductorElection) leaseConfig() leaderelection.LeaderElectionConfig {
	return leaderelection.LeaderElectionConfig{
		Name:            ko.Name(),
		Lock:            ko.leaseLock(),
		LeaseDuration:   ko.opts.LeaseDuration,
		RenewDeadline:   ko.opts.RenewDeadline,
		RetryPeriod:     ko.opts.RetryPeriod,
		ReleaseOnCancel: true,
		Callbacks:       ko.callbacks(),
	}
}

// Lease lock
func (ko *KonductorElection) leaseLock() *resourcelock.LeaseLock {
	opts := ko.opts
	return &resourcelock.LeaseLock{
		LeaseMeta: metav1.ObjectMeta{
			Name:        ko.name,
			Namespace:   opts.Namespace,
			Annotations: opts.Annotations,
			Labels:      opts.Labels,
		},
		Client: ko.kube.Clientset().CoordinationV1(),
		LockConfig: resourcelock.ResourceLockConfig{
			Identity:      hostname(),
			EventRecorder: ko.event.Recorder(),
		},
	}
}

// Build callbacks
func (ko *KonductorElection) callbacks() leaderelection.LeaderCallbacks {
	return leaderelection.LeaderCallbacks{
		OnStartedLeading: func(ctx context.Context) {
			if ko.event.Recorder() != nil {
				ko.event.Recorder().Eventf(
					&corev1.ObjectReference{
						Name:      ko.name,
						Namespace: ko.opts.Namespace,
						Kind:      ko.kind(),
					}, corev1.EventTypeNormal, "KonductorElected", "%s became konductor", hostname(),
				)
			}

			// Run the actual kordinator
			// With a cancellable context - useful for OnStoppedLeading
			runCtx, cancel := context.WithCancel(ctx)
			ko.runCancel = cancel

			ko.election.konductor = hostname()
			if ko.election.onElected != nil {
				ko.election.onElected(ko.election.konductor)
			}

			ko.election.startedKonducting.Store(true)

			logger.Info().Msgf("%s 🏆 became konductor, starting kordinator...", ko.election.konductor)

			ko.run(runCtx)
		},
		OnStoppedLeading: func() {
			if ko.event.Recorder() != nil {
				ko.event.Recorder().Eventf(
					&corev1.ObjectReference{
						Name:      ko.name,
						Namespace: ko.opts.Namespace,
						Kind:      ko.kind(),
					}, corev1.EventTypeWarning, "KonductorLost", "%s lost konducting", hostname(),
				)
			}
			if ko.election.startedKonducting.Load() {
				// Cancel the run context
				if ko.runCancel != nil {
					ko.runCancel()
					ko.runCancel = nil // reset to always reflect current leadership session only
				}

				logger.Info().Msg("Performing cleanup on the actual konductor...")
				ko.election.konductor = ""
				ko.election.startedKonducting.Store(false)
			} else {
				logger.Info().Msg("No cleanup needed as we never started konducting.")
			}

			logger.Info().Msgf("%s 👋 Stopped konducting - lease released", hostname())
		},
		OnNewLeader: func(identity string) {
			if ko.event.Recorder() != nil {
				ko.event.Recorder().Eventf(
					&corev1.ObjectReference{
						Name:      ko.name,
						Namespace: ko.opts.Namespace,
						Kind:      ko.kind(),
					}, corev1.EventTypeNormal, "NewKonductorElected", "%s elected as konductor", hostname(),
				)
			}
			logger.Info().Msgf("👑 New konductor elected: %s", identity)
		},
	}
}

// Get hostname
func hostname() string {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = uuid.New().String()
	}
	return hostname
}

// Konductor returns the instance that won the konductor election
func (ko *KonductorElection) Konductor() string {
	return ko.election.konductor
}
