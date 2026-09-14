// webhook/infrastructure.go
//
// Housekeeper reconcilers for infrastructure security state.
//
// Two resources are applied once at startup by cmd/internal.ensureSecurity but
// have no ongoing reconciler — a human can silently break security by removing
// them after startup:
//
//  1. Namespace labels — the deletion-protection webhook uses ObjectSelector to
//     narrow to labeled resources. Removing the orkestra.io/deletion-protection
//     label from the operator namespace means the webhook no longer intercepts
//     deletion attempts against the namespace itself. The safety ticker catches
//     this at the normal reconcile cadence.
//
//  2. CRD conversion caBundle — conversion webhooks require the caBundle in the
//     CRD's spec.conversion.webhook.clientConfig.caBundle. If stripped, any CR
//     at an old API version fails conversion immediately. This is watched with a
//     dedicated goroutine so the restore happens within a single API round-trip,
//     not at the next safety-ticker interval.
//
// Kubernetes API server semantics guarantee no reconcile loop: if the patcher
// produces no change to the stored object (caBundle already correct), no MODIFIED
// event is emitted. So the event chain terminates after at most two reconciles.
package webhook

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"time"

	orklabels "github.com/orkspace/orkestra/pkg/labels"
	"github.com/orkspace/orkestra/pkg/logger"
	"github.com/orkspace/orkestra/pkg/metrics"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
)

const (
	infraNamespaceLabel   = "namespace-labels"
	infraConversionBundle = "conversion-crd-cabundle"
)

// ── CRD watcher interface ──────────────────────────────────────────────────────

// CRDWatcher provides CRD watch capability without importing the apiextensions
// package into this package. Implemented by the caller (cmd/internal) using the
// apiextensions client.
type CRDWatcher interface {
	Watch(ctx context.Context, crdName string) (watch.Interface, error)
}

// ConversionCRDPatchFn patches the caBundle on a single CRD's conversion webhook.
// Called by the housekeeper for each CRD that declares conversion.updateCRD: true.
// The closure captures serviceName, serviceNamespace, and the apiextensions client.
type ConversionCRDPatchFn func(ctx context.Context, crdName, caBundle64, storageVersion string) error

// SetConversionCRDPatcher provides the housekeeper with the function it needs to
// re-patch CRD conversion webhooks. Called from cmd/internal after the kubeclient
// and katalog are available.
func (ws *WebhookServer) SetConversionCRDPatcher(fn ConversionCRDPatchFn) {
	ws.patchConversionCRD = fn
}

// SetCRDWatcher provides the housekeeper with the CRD watch capability. Called
// from cmd/internal when conversion is enabled. Without this, the conversion CRD
// reconciler still runs on the safety ticker — the watcher is the fast path only.
func (ws *WebhookServer) SetCRDWatcher(w CRDWatcher) {
	ws.crdWatcher = w
}

// ── Namespace label reconciler ─────────────────────────────────────────────────

// reconcileNamespaceLabels ensures the operator namespace carries the Orkestra
// resource labels required for the deletion-protection webhook's ObjectSelector.
// No-op when deletion protection is not enabled.
func (ws *WebhookServer) reconcileNamespaceLabels() {
	if !ws.deletionProtection.Load() {
		return
	}
	if ws.kubeClient == nil {
		return
	}

	namespace := ws.operatorNamespace()
	if namespace == "" {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), lowTimeout)
	defer cancel()

	ns, err := ws.kubeClient.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{
		ResourceVersion: "0", // watch cache — avoids etcd round-trip
	})
	if err != nil {
		if !k8serrors.IsNotFound(err) {
			logger.Error().Err(err).Str("namespace", namespace).
				Msg("housekeeper: failed to read namespace for label check")
			metrics.RecordWebhookReconciliationFailure(infraNamespaceLabel)
		}
		return
	}

	required := orklabels.OrkestraResourceLabels()
	if labelsPresent(ns.Labels, required) {
		return // already correct — no write needed, no MODIFIED event
	}

	patch := map[string]interface{}{
		"metadata": map[string]interface{}{
			"labels": required,
		},
	}
	patchBytes, err := json.Marshal(patch)
	if err != nil {
		logger.Error().Err(err).Msg("housekeeper: failed to marshal namespace label patch")
		metrics.RecordWebhookReconciliationFailure(infraNamespaceLabel)
		return
	}

	_, err = ws.kubeClient.CoreV1().Namespaces().Patch(
		ctx, namespace, types.MergePatchType, patchBytes, metav1.PatchOptions{},
	)
	if err != nil && !k8serrors.IsNotFound(err) {
		logger.Error().Err(err).Str("namespace", namespace).
			Msg("housekeeper: failed to restore namespace labels")
		metrics.RecordWebhookReconciliationFailure(infraNamespaceLabel)
		return
	}

	logger.Warn().Str("namespace", namespace).
		Msg("housekeeper: namespace labels restored — deletion-protection was unprotected")
	metrics.RecordWebhookReconciled(infraNamespaceLabel)
}

// labelsPresent returns true when every required label exists in current with
// the same value. Extra labels in current are irrelevant.
func labelsPresent(current, required map[string]string) bool {
	for k, v := range required {
		if current[k] != v {
			return false
		}
	}
	return true
}

// operatorNamespace returns the namespace the operator is running in.
// Uses certSecretNamespace when certs were auto-generated; falls back to konfig.
func (ws *WebhookServer) operatorNamespace() string {
	if ws.certSecretNamespace != "" {
		return ws.certSecretNamespace
	}
	if ws.konfig != nil {
		return ws.konfig.Cluster().Namespace()
	}
	return ""
}

// ── CRD conversion caBundle reconciler ────────────────────────────────────────

// reconcileCRDConversionWebhooks ensures every CRD with conversion.updateCRD: true
// has the correct caBundle in its spec.conversion.webhook.clientConfig.caBundle.
// No-op when conversion is not enabled or no patcher is registered.
func (ws *WebhookServer) reconcileCRDConversionWebhooks() {
	if !ws.convEnabled {
		return
	}
	if ws.patchConversionCRD == nil || ws.katalog == nil {
		return
	}

	var caPEM []byte
	if ws.certSecretData != nil {
		caPEM = ws.certSecretData.caPEM
	} else {
		var err error
		caPEM, err = readCABundle(ws.hookReg.TLSCertFile)
		if err != nil {
			logger.Error().Err(err).
				Msg("housekeeper: cannot read CA bundle for CRD conversion patch")
			metrics.RecordWebhookReconciliationFailure(infraConversionBundle)
			return
		}
	}

	caBundle64 := base64.StdEncoding.EncodeToString(caPEM)

	ctx, cancel := context.WithTimeout(context.Background(), highTimeout)
	defer cancel()

	for _, crd := range ws.katalog.EnabledCRDs() {
		if crd.Conversion == nil || !crd.UpdateCRDCaBundle() {
			continue
		}

		crdName := crd.APITypes.Plural + "." + crd.APITypes.Group
		storageVersion := crd.Conversion.StorageVersion

		if err := ws.patchConversionCRD(ctx, crdName, caBundle64, storageVersion); err != nil {
			logger.Error().Err(err).Str("crd", crdName).
				Msg("housekeeper: failed to patch CRD conversion caBundle")
			metrics.RecordWebhookReconciliationFailure(infraConversionBundle)
			continue
		}

		logger.Debug().Str("crd", crdName).
			Msg("housekeeper: CRD conversion caBundle reconciled")
	}
}

// ── CRD conversion watcher ────────────────────────────────────────────────────

// watchConversionCRDs starts one goroutine per conversion CRD that watches for
// MODIFIED events. Any modification triggers an immediate reconcile so a stripped
// caBundle is restored within a single API round-trip rather than at the next
// safety-ticker interval.
//
// No-op when no CRD watcher is set or conversion is not enabled.
func (ws *WebhookServer) watchConversionCRDs(ctx context.Context, trigger chan<- struct{}) {
	if ws.crdWatcher == nil || !ws.convEnabled || ws.katalog == nil {
		return
	}

	for _, crd := range ws.katalog.EnabledCRDs() {
		if crd.Conversion == nil || !crd.UpdateCRDCaBundle() {
			continue
		}
		crdName := crd.APITypes.Plural + "." + crd.APITypes.Group
		go ws.watchSingleConversionCRD(ctx, trigger, crdName)
	}
}

// watchSingleConversionCRD watches one CRD for MODIFIED events and signals
// trigger on any change. Reconnects automatically on stream expiry or error.
func (ws *WebhookServer) watchSingleConversionCRD(ctx context.Context, trigger chan<- struct{}, crdName string) {
	for {
		w, err := ws.crdWatcher.Watch(ctx, crdName)
		if err != nil {
			logger.Warn().Err(err).Str("crd", crdName).
				Msg("housekeeper watch (conversion-crd): failed to start, retrying")
			select {
			case <-ctx.Done():
				return
			case <-time.After(watchRetryDelay):
				continue
			}
		}

		for {
			select {
			case <-ctx.Done():
				w.Stop()
				return
			case event, ok := <-w.ResultChan():
				if !ok {
					goto reconnect
				}
				if event.Type == watch.Modified {
					logger.Warn().Str("crd", crdName).
						Msg("housekeeper: conversion CRD modified — triggering caBundle reconcile")
					select {
					case trigger <- struct{}{}:
					default: // already pending
					}
				}
			}
		}

	reconnect:
		w.Stop()
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
}
