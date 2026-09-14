package validate

import (
	"github.com/orkspace/orkestra/pkg/katalog"
	"github.com/orkspace/orkestra/pkg/konfig"
)

type executor struct {
	k   *katalog.Katalog
	kfg *konfig.Konfig
}

// Execute runs the full validation pipeline on k, mutating it in place.
// k must have already been populated via KomposeRuntimeKatalog.
func Execute(k *katalog.Katalog, kfg *konfig.Konfig) error {
	e := &executor{k: k, kfg: kfg}
	return e.run()
}

// DetectCycles exposes dependency cycle detection for external packages (integration tests).
func DetectCycles(k *katalog.Katalog) error {
	return (&executor{k: k}).detectDependencyCycles()
}

// ValidateGatewayClusters validates the gateway clusters configuration.
func ValidateGatewayClusters(k *katalog.Katalog) error {
	return (&executor{k: k}).ValidateGatewayClusters()
}

// ValidateServe validates the serve configuration across all CRDs.
func ValidateServe(k *katalog.Katalog) error {
	return (&executor{k: k}).ValidateServe()
}

func (e *executor) run() error {
	// 1. Struct-level field validation
	if valErr := konfig.Validate().Struct(e.k); valErr != nil {
		e.handleValidationErrors(valErr)
		return valErr
	}

	// 2. Lifecycle
	if err := e.validateLifecycle(); err != nil {
		return err
	}

	// 3. Policy
	if err := e.validatePolicy(); err != nil {
		return err
	}

	// 4. Uniqueness
	if err := e.validateUniqueness(); err != nil {
		return err
	}

	// 5. dependsOn (existence + cycle detection)
	if err := e.validateDependsOn(); err != nil {
		return err
	}

	// 6. Reconciler config
	if err := e.validateReconciler(); err != nil {
		return err
	}

	// 7. Reconcilers
	if err := e.k.AddReconcilers(); err != nil {
		return err
	}

	// 8. Target constructors
	if err := e.k.AddTargetConstructors(); err != nil {
		return err
	}

	// 9. Runtime objects
	if err := e.k.AddRuntimeObjects(); err != nil {
		return err
	}

	// 10. Hooks
	if err := e.k.AddHooks(); err != nil {
		return err
	}

	// 11. Target hooks
	if err := e.k.AddTargetHooks(); err != nil {
		return err
	}

	// 12. Status
	e.validateStatus()

	// 13. Autoscale profile
	if err := e.validateAutoscaleProfile(); err != nil {
		return err
	}

	// 14. Resource profile
	if err := e.validateResourceProfile(); err != nil {
		return err
	}

	// 15. Probe profiles
	if err := e.validateProbeProfiles(); err != nil {
		return err
	}

	// 16. Autoscaler metrics
	if err := e.validateAutoscalerMetrics(); err != nil {
		return err
	}

	// 17. Namespace protection
	if err := e.validateNamespaceProtection(); err != nil {
		return err
	}

	// 18. Time duration
	if err := e.validateTimeDuration(); err != nil {
		return err
	}

	// 19. HPA reference
	if err := e.validateHPAReference(); err != nil {
		return err
	}

	// 20. Teams
	if err := e.validateTeams(); err != nil {
		return err
	}

	// 21. Status types
	if err := e.validateStatusTypes(); err != nil {
		return err
	}

	// 22. Services
	if err := e.validateService(); err != nil {
		return err
	}

	// 23. Custom resources
	if err := e.validateCustomResources(); err != nil {
		return err
	}

	// 24. Gateway requirement
	if err := e.validateGateway(); err != nil {
		return err
	}

	// 25. Enrich config
	if err := e.validateEnrich(); err != nil {
		return err
	}

	// 26. Deletion protection
	if err := e.validateDeletionProtection(); err != nil {
		return err
	}

	// 27. Security profiles
	if err := e.validateSecurityProfiles(); err != nil {
		return err
	}

	// 28. Capability names
	if err := e.validateSecurityCapabilities(); err != nil {
		return err
	}

	// 29. User profiles
	if err := e.validateUserProfiles(); err != nil {
		return err
	}

	// 30. HPA behavior profiles
	if err := e.validateHPABehaviorProfiles(); err != nil {
		return err
	}

	// 31. User notes
	if err := e.validateUserNotes(); err != nil {
		return err
	}

	// 32. PDB behavior profiles
	if err := e.validatePDBBehaviorProfiles(); err != nil {
		return err
	}

	// 33. Rolling update profiles
	if err := e.validateRollingUpdateProfiles(); err != nil {
		return err
	}

	// 34. NetworkPolicy profiles
	if err := e.validateNetworkPolicyProfiles(); err != nil {
		return err
	}

	// 35. ResourceQuota profiles
	if err := e.validateResourceQuotaProfiles(); err != nil {
		return err
	}

	// 36. LimitRange profiles
	if err := e.validateLimitRangeProfiles(); err != nil {
		return err
	}

	// 37. Cross-namespace ops
	if err := e.validateCrossNamespaceOps(); err != nil {
		return err
	}

	// 38. Port protocols
	if err := e.validatePortProtocols(); err != nil {
		return err
	}

	// 39. Workload autoscale
	if err := e.validateWorkloadAutoscale(); err != nil {
		return err
	}

	// 40. External calls
	if err := e.validateExternalCalls(); err != nil {
		return err
	}

	// 41. Watch entries
	if err := e.validateWatchEntries(); err != nil {
		return err
	}

	// 42. CRD entry labels
	if err := e.validateCRDEntryLabels(); err != nil {
		return err
	}

	// 43. envFrom refs
	if err := e.validateEnvFromRefs(); err != nil {
		return err
	}

	// 44. Serve config
	if err := e.ValidateServe(); err != nil {
		return err
	}

	// 45. Admission rules
	if err := e.validateAdmissionRules(); err != nil {
		return err
	}

	// 46. Gateway tokens
	if err := e.validateGatewayTokens(); err != nil {
		return err
	}

	// 47. Gateway webhooks
	if err := e.validateGatewayWebhooks(); err != nil {
		return err
	}

	// 48. Gateway clusters
	if err := e.ValidateGatewayClusters(); err != nil {
		return err
	}

	// 49. Publish config
	if err := e.validatePublish(); err != nil {
		return err
	}

	// 50. Retry backoff
	if err := e.validateRetryBackoff(); err != nil {
		return err
	}

	// 51. Requeue
	if err := e.validateRequeue(); err != nil {
		return err
	}

	// 52. Cross declaration
	if err := e.validateCrossDecl(); err != nil {
		return err
	}

	// 53. PreReconcile
	if err := e.validatePreReconcile(); err != nil {
		return err
	}

	// 54. Events
	if err := e.validateEventEntries(); err != nil {
		return err
	}

	return nil
}
