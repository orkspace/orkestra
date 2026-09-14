package katalog

import (
	"fmt"

	"github.com/orkspace/orkestra/domain"
	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// ---------------------------------------------------------------------------------
// Add reconcilers
func (k *Katalog) AddReconcilers() error {
	for name, crd := range k.enabledCRDs {
		rc := crd.OperatorBox

		// Add providers block
		if len(rc.ProviderBlocks) > 0 {
			blocks, err := orktypes.ParseProviderBlocks(rc.RawProviders)
			if err != nil {
				return err
			}
			rc.ProviderBlocks = blocks
		}

		if !crd.IsDynamic() {
			if crd.DefaultReconcile() {
				// Wire per-target entries that opt out of the default reconciler
				// (reconciler.default: false). These use the same ReconcilerRegistry
				// keyed by the CRD's GVK, but are stored on the target's OperatorBox.
				if err := wirePerTargetConstructors(name, &crd); err != nil {
					return err
				}
				crd.OperatorBox = rc
				k.enabledCRDs[name] = crd
				continue
			}

			constructorFn, ok := orktypes.ReconcilerRegistry[crd.GroupVersionKind]
			if !ok {
				return fmt.Errorf(
					"CRD %q: no constructor registered — "+
						"check reconciler.constructor in Katalog and re-run ork generate registry",
					name,
				)
			}

			rc.Constructor = constructorFn
		}

		crd.OperatorBox = rc
		k.enabledCRDs[name] = crd
	}
	return nil
}

// ---------------------------------------------------------------------------------
// Add hooks
func (k *Katalog) AddHooks() error {
	for name, crd := range k.enabledCRDs {
		if !crd.DefaultReconcile() {
			continue
		}
		hookFn, ok := orktypes.HookRegistry[crd.GroupVersionKind]
		if ok {
			crd.OperatorBox.HookFactory = hookFn
		}

		if !crd.HasServeTargetEntries() {
			k.enabledCRDs[name] = crd
			continue
		}

		// Per-target hook validation: a target that declares hooks at the same
		// location as the CRD-level binary (or just overrides args) relies on the
		// CRD-level factory — it must be registered. Targets with a distinct binary
		// are validated separately by addTargetHooks.
		crdLevelHookLoc := ""
		if crd.OperatorBox.Reconciler != nil && crd.OperatorBox.Reconciler.Hooks != nil {
			crdLevelHookLoc = crd.OperatorBox.Reconciler.Hooks.Location
		}
		for targetName, targetCfg := range crd.Serve.Target.Entries {
			if targetCfg.OperatorBox == nil || targetCfg.OperatorBox.Reconciler == nil {
				continue
			}
			h := targetCfg.OperatorBox.Reconciler.Hooks
			if h == nil {
				continue
			}
			// Distinct binary → addTargetHooks validates; skip here.
			if h.Location != "" && h.Location != crdLevelHookLoc {
				continue
			}
			// Same binary (or args-only override) — CRD-level factory must be registered.
			if (h.Location != "" || h.Function != "") && !ok {
				return fmt.Errorf(
					"CRD %q target %q: reconciler.hooks declared but no hook factory registered for GVK %s — "+
						"re-run ork generate registry",
					name, targetName, crd.GroupVersionKind,
				)
			}
		}

		k.enabledCRDs[name] = crd
	}
	return nil
}

// ---------------------------------------------------------------------------------
// addTargetHooks wires per-target hook factories from TargetHookRegistry onto
// CRDEntry.TargetHookFactories. Only targets that declare a distinct hook binary
// (different location from the CRD-level hooks) need an entry here — targets that
// share the CRD-level binary and only override args are handled at reconcile time
// by mergeReconcilerConfig inside EffectiveOperatorBox.
func (k *Katalog) AddTargetHooks() error {
	for name, crd := range k.enabledCRDs {
		if !crd.HasServeTargetEntries() {
			continue
		}
		crdLevelLocation := ""
		if crd.OperatorBox.Reconciler != nil && crd.OperatorBox.Reconciler.Hooks != nil {
			crdLevelLocation = crd.OperatorBox.Reconciler.Hooks.Location
		}
		gvk := crd.GroupVersionKind
		for targetName, targetCfg := range crd.Serve.Target.Entries {
			if targetCfg.OperatorBox == nil || targetCfg.OperatorBox.Reconciler == nil {
				continue
			}
			h := targetCfg.OperatorBox.Reconciler.Hooks
			if h == nil || h.Location == "" || h.Location == crdLevelLocation {
				continue
			}
			targetMap, ok := orktypes.TargetHookRegistry[gvk]
			if !ok {
				return fmt.Errorf(
					"CRD %q target %q: per-target hooks (location %q) declared but "+
						"no TargetHookRegistry entry for this GVK — re-run ork generate registry",
					name, targetName, h.Location,
				)
			}
			fn, ok := targetMap[targetName]
			if !ok {
				return fmt.Errorf(
					"CRD %q target %q: no TargetHookRegistry entry for this target — "+
						"re-run ork generate registry",
					name, targetName,
				)
			}
			if crd.TargetHookFactories == nil {
				crd.TargetHookFactories = make(map[string]func() domain.AnyReconcileHooks)
			}
			crd.TargetHookFactories[targetName] = fn
		}
		k.enabledCRDs[name] = crd
	}
	return nil
}

// ---------------------------------------------------------------------------------
// wirePerTargetConstructors wires constructors for per-target entries that have
// reconciler.default: false set on their own OperatorBox. These targets share the
// CRD-level GVK and look up their constructor in ReconcilerRegistry, storing it
// directly on targetCfg.OperatorBox.Constructor.
func wirePerTargetConstructors(crdName string, crd *orktypes.CRDEntry) error {
	if !crd.HasServeTargetEntries() {
		return nil
	}
	for targetName, targetCfg := range crd.Serve.Target.Entries {
		if targetCfg.OperatorBox == nil || targetCfg.OperatorBox.Reconciler == nil {
			continue
		}
		rec := targetCfg.OperatorBox.Reconciler
		if rec.Default == nil || *rec.Default || rec.ConstructorDecl == nil {
			continue
		}
		fn, ok := orktypes.ReconcilerRegistry[crd.GroupVersionKind]
		if !ok {
			return fmt.Errorf(
				"CRD %q target %q: reconciler.default: false declared but "+
					"no ReconcilerRegistry entry for this GVK — re-run ork generate registry",
				crdName, targetName,
			)
		}
		targetCfg.OperatorBox.Constructor = fn
		crd.Serve.Target.Entries[targetName] = targetCfg
	}
	return nil
}

// addTargetConstructors wires per-target constructor factories from
// TargetReconcilerRegistry onto CRDEntry.TargetReconcilerFactories.
func (k *Katalog) AddTargetConstructors() error {
	for name, crd := range k.enabledCRDs {
		if !crd.HasServeTargetEntries() {
			continue
		}
		gvk := crd.GroupVersionKind
		for targetName, targetCfg := range crd.Serve.Target.Entries {
			if targetCfg.OperatorBox == nil || targetCfg.OperatorBox.Reconciler == nil {
				continue
			}
			rec := targetCfg.OperatorBox.Reconciler
			if rec.Default == nil || *rec.Default || rec.ConstructorDecl == nil {
				continue
			}
			targetMap, ok := orktypes.TargetReconcilerRegistry[gvk]
			if !ok {
				return fmt.Errorf(
					"CRD %q target %q: reconciler.default: false declared but "+
						"no TargetReconcilerRegistry entry for this GVK — re-run ork generate registry",
					name, targetName,
				)
			}
			fn, ok := targetMap[targetName]
			if !ok {
				return fmt.Errorf(
					"CRD %q target %q: no TargetReconcilerRegistry entry for this target — "+
						"re-run ork generate registry",
					name, targetName,
				)
			}
			if crd.TargetReconcilerFactories == nil {
				crd.TargetReconcilerFactories = make(map[string]orktypes.NewReconcilerFunc)
			}
			crd.TargetReconcilerFactories[targetName] = fn
		}
		k.enabledCRDs[name] = crd
	}
	return nil
}
