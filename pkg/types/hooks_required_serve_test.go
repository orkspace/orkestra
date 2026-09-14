package types

import "testing"

func findValidationRule(t *testing.T, rules []ValidationRule, field string) ValidationRule {
	t.Helper()
	for _, r := range rules {
		if r.Field == field {
			return r
		}
	}
	t.Fatalf("no rule found for field %q in %+v", field, rules)
	return ValidationRule{}
}

func findMutationRule(t *testing.T, rules []MutationRule, field string) MutationRule {
	t.Helper()
	for _, r := range rules {
		if r.Field == field {
			return r
		}
	}
	t.Fatalf("no rule found for field %q in %+v", field, rules)
	return MutationRule{}
}

func TestRequiredServeFieldRules_NilCases(t *testing.T) {
	t.Run("nil CRDEntry", func(t *testing.T) {
		var c *CRDEntry
		if got := c.RequiredServeFieldRules(); got != nil {
			t.Errorf("got %v, want nil", got)
		}
	})
	t.Run("nil serve", func(t *testing.T) {
		c := &CRDEntry{}
		if got := c.RequiredServeFieldRules(); got != nil {
			t.Errorf("got %v, want nil", got)
		}
	})
	t.Run("serve with no required fields", func(t *testing.T) {
		c := &CRDEntry{Serve: &ServeConfig{
			Fields: map[string]ServeFieldConfig{
				"image": {Label: "Container Image"},
			},
		}}
		if got := c.RequiredServeFieldRules(); got != nil {
			t.Errorf("got %v, want nil — nothing is required", got)
		}
	})
}

func TestRequiredServeFieldRules_SpecField(t *testing.T) {
	c := &CRDEntry{Serve: &ServeConfig{
		Fields: map[string]ServeFieldConfig{
			"team":  {Label: "Team", Required: true},
			"image": {Label: "Container Image"}, // not required — no rule
		},
	}}

	rules := c.RequiredServeFieldRules()
	if len(rules) != 1 {
		t.Fatalf("got %d rules, want 1: %+v", len(rules), rules)
	}

	r := rules[0]
	if r.Field != "spec.team" {
		t.Errorf("Field = %q, want %q", r.Field, "spec.team")
	}
	if r.Operator != ConditionExists {
		t.Errorf("Operator = %q, want %q", r.Operator, ConditionExists)
	}
	if r.Message != "Team is required" {
		t.Errorf("Message = %q, want %q", r.Message, "Team is required")
	}
	if r.Action != ValidationActionDeny {
		t.Errorf("Action = %q, want %q", r.Action, ValidationActionDeny)
	}
}

func TestRequiredServeFieldRules_InheritsWhenAndOr(t *testing.T) {
	appOnly := []Condition{{Field: "spec.workloadType", Equals: "app"}}
	certOrApp := []Condition{
		{Field: "spec.workloadType", Equals: "cert"},
		{Field: "spec.workloadType", Equals: "app"},
	}

	c := &CRDEntry{Serve: &ServeConfig{
		Fields: map[string]ServeFieldConfig{
			// A discriminator-routed field: only relevant (and only required)
			// when workloadType: app — the same When already used to hide it
			// from the form for other workload types.
			"repoURL": {Label: "Repository URL", Required: true, When: appOnly},
			// Or carries through the same way.
			"domain": {Label: "Domain", Required: true, Or: certOrApp},
			// A universal field with no condition at all.
			"team": {Label: "Team", Required: true},
		},
	}}

	rules := c.RequiredServeFieldRules()
	if len(rules) != 3 {
		t.Fatalf("got %d rules, want 3: %+v", len(rules), rules)
	}

	repoRule := findValidationRule(t, rules, "spec.repoURL")
	if len(repoRule.When) != 1 || repoRule.When[0].Field != "spec.workloadType" || repoRule.When[0].Equals != "app" {
		t.Errorf("repoURL rule.When = %+v, want the field's own When condition carried through", repoRule.When)
	}
	if len(repoRule.Or) != 0 {
		t.Errorf("repoURL rule.Or = %+v, want empty — the field declared When, not Or", repoRule.Or)
	}

	domainRule := findValidationRule(t, rules, "spec.domain")
	if len(domainRule.Or) != 2 {
		t.Errorf("domain rule.Or = %+v, want the field's own Or conditions carried through", domainRule.Or)
	}

	teamRule := findValidationRule(t, rules, "spec.team")
	if len(teamRule.When) != 0 || len(teamRule.Or) != 0 {
		t.Errorf("team rule When/Or = %+v/%+v, want both empty — the field declared neither", teamRule.When, teamRule.Or)
	}
}

func TestRequiredServeFieldRules_LabelFallsBackToFieldName(t *testing.T) {
	c := &CRDEntry{Serve: &ServeConfig{
		Fields: map[string]ServeFieldConfig{
			"team": {Required: true}, // no Label set
		},
	}}

	rules := c.RequiredServeFieldRules()
	if len(rules) != 1 || rules[0].Message != "team is required" {
		t.Errorf("got %+v, want message to fall back to the raw field name", rules)
	}
}

func TestRequiredServeFieldRules_LabelsAndAnnotationsUseNotes(t *testing.T) {
	c := &CRDEntry{Serve: &ServeConfig{
		Labels: map[string]ServeFieldConfig{
			"team": {Label: "Team", Required: true},
		},
		Annotations: map[string]ServeFieldConfig{
			"platform.myorg.io/jira-ticket": {Label: "Jira Ticket", Required: true},
		},
	}}

	rules := c.RequiredServeFieldRules()
	if len(rules) != 2 {
		t.Fatalf("got %d rules, want 2: %+v", len(rules), rules)
	}

	labelRule := findValidationRule(t, rules, `{{ getLabel . "team" }}`)
	if labelRule.Message != "Team is required" || labelRule.Operator != ConditionExists {
		t.Errorf("label rule = %+v, want a getLabel-based exists rule", labelRule)
	}

	// A dotted annotation key must go through getAnnotation, not a raw
	// dot-path — dot-path resolution would misparse the dots inside the key
	// itself as extra path segments.
	annotationField := `{{ getAnnotation . "platform.myorg.io/jira-ticket" }}`
	annotationRule := findValidationRule(t, rules, annotationField)
	if annotationRule.Message != "Jira Ticket is required" || annotationRule.Operator != ConditionExists {
		t.Errorf("annotation rule = %+v, want a getAnnotation-based exists rule", annotationRule)
	}
}

func TestRequiredServeFieldRules_LinkSetForLabelsAndAnnotationsOnly(t *testing.T) {
	c := &CRDEntry{Serve: &ServeConfig{
		Fields: map[string]ServeFieldConfig{
			"team": {Label: "Team", Required: true},
		},
		Labels: map[string]ServeFieldConfig{
			"environment": {Label: "Environment", Required: true},
		},
		Annotations: map[string]ServeFieldConfig{
			"platform.myorg.io/jira-ticket": {Label: "Jira Ticket", Required: true},
		},
	}}

	rules := c.RequiredServeFieldRules()

	specRule := findValidationRule(t, rules, "spec.team")
	if specRule.Link != "" {
		t.Errorf("spec field rule.Link = %q, want empty — \"spec.team\" is already a clean display name", specRule.Link)
	}

	labelRule := findValidationRule(t, rules, `{{ getLabel . "environment" }}`)
	if labelRule.Link != "environment" {
		t.Errorf("label field rule.Link = %q, want %q", labelRule.Link, "environment")
	}

	annotationRule := findValidationRule(t, rules, `{{ getAnnotation . "platform.myorg.io/jira-ticket" }}`)
	if annotationRule.Link != "platform.myorg.io/jira-ticket" {
		t.Errorf("annotation field rule.Link = %q, want %q", annotationRule.Link, "platform.myorg.io/jira-ticket")
	}
}

func TestAllServeFieldRefs_SortedByOrderThenName(t *testing.T) {
	c := &CRDEntry{Serve: &ServeConfig{
		Fields: map[string]ServeFieldConfig{
			"zebra": {Order: 2},
			"alpha": {}, // unset (0) — sorts after all explicitly ordered fields
		},
		Labels: map[string]ServeFieldConfig{
			"team": {Order: 1},
		},
	}}

	refs := c.allServeFieldRefs()
	if len(refs) != 3 {
		t.Fatalf("got %d refs, want 3", len(refs))
	}
	got := []string{refs[0].name, refs[1].name, refs[2].name}
	want := []string{"team", "zebra", "alpha"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("refs[%d].name = %q, want %q (full order: %v)", i, got[i], want[i], got)
		}
	}
}

func TestDuplicateServeFieldOrders_NilCases(t *testing.T) {
	t.Run("nil CRDEntry", func(t *testing.T) {
		var c *CRDEntry
		if got := c.DuplicateServeFieldOrders(); got != nil {
			t.Errorf("got %v, want nil", got)
		}
	})
	t.Run("nil serve", func(t *testing.T) {
		c := &CRDEntry{}
		if got := c.DuplicateServeFieldOrders(); got != nil {
			t.Errorf("got %v, want nil", got)
		}
	})
}

func TestDuplicateServeFieldOrders_NoCollision(t *testing.T) {
	c := &CRDEntry{Serve: &ServeConfig{
		Fields: map[string]ServeFieldConfig{
			"image":    {Order: 1},
			"replicas": {Order: 2},
		},
	}}
	if got := c.DuplicateServeFieldOrders(); len(got) != 0 {
		t.Errorf("got %v, want no collisions", got)
	}
}

func TestDuplicateServeFieldOrders_UnsetNeverCollides(t *testing.T) {
	c := &CRDEntry{Serve: &ServeConfig{
		Fields: map[string]ServeFieldConfig{
			"image":    {}, // order: 0
			"replicas": {}, // order: 0
		},
	}}
	if got := c.DuplicateServeFieldOrders(); len(got) != 0 {
		t.Errorf("got %v, want no collisions — order: 0 is \"unset\", not a real position", got)
	}
}

func TestDuplicateServeFieldOrders_CollisionAcrossBuckets(t *testing.T) {
	c := &CRDEntry{Serve: &ServeConfig{
		Fields: map[string]ServeFieldConfig{
			"image": {Order: 3},
		},
		Labels: map[string]ServeFieldConfig{
			"team": {Order: 3},
		},
	}}
	got := c.DuplicateServeFieldOrders()
	if len(got) != 1 {
		t.Fatalf("got %d colliding orders, want 1: %+v", len(got), got)
	}
	names := got[3]
	if len(names) != 2 || names[0] != "image" || names[1] != "team" {
		t.Errorf("colliding names at order 3 = %v, want [image team] (sorted)", names)
	}
}

func TestRequiredServeFieldRules_AllThreeBucketsCombine(t *testing.T) {
	c := &CRDEntry{Serve: &ServeConfig{
		Fields: map[string]ServeFieldConfig{
			"image": {Label: "Container Image", Required: true},
		},
		Labels: map[string]ServeFieldConfig{
			"team": {Label: "Team", Required: true},
		},
		Annotations: map[string]ServeFieldConfig{
			"expose": {Label: "Expose externally", Required: true},
		},
	}}

	rules := c.RequiredServeFieldRules()
	if len(rules) != 3 {
		t.Fatalf("got %d rules, want 3 (one per bucket): %+v", len(rules), rules)
	}
	findValidationRule(t, rules, "spec.image")
	findValidationRule(t, rules, `{{ getLabel . "team" }}`)
	findValidationRule(t, rules, `{{ getAnnotation . "expose" }}`)
}

func TestEnumServeFieldRules_NilCases(t *testing.T) {
	t.Run("nil CRDEntry", func(t *testing.T) {
		var c *CRDEntry
		if got := c.EnumServeFieldRules(); got != nil {
			t.Errorf("got %v, want nil", got)
		}
	})
	t.Run("no enum fields", func(t *testing.T) {
		c := &CRDEntry{Serve: &ServeConfig{
			Fields: map[string]ServeFieldConfig{
				"team": {Label: "Team", Required: true},
			},
		}}
		if got := c.EnumServeFieldRules(); got != nil {
			t.Errorf("got %v, want nil — nothing is type: enum", got)
		}
	})
	t.Run("type: enum with no enum values declared — skipped", func(t *testing.T) {
		c := &CRDEntry{Serve: &ServeConfig{
			Fields: map[string]ServeFieldConfig{
				"workloadType": {Label: "Workload Type", Type: "enum"},
			},
		}}
		if got := c.EnumServeFieldRules(); got != nil {
			t.Errorf("got %v, want nil — enum list is empty", got)
		}
	})
}

func TestEnumServeFieldRules_SpecField(t *testing.T) {
	c := &CRDEntry{Serve: &ServeConfig{
		Fields: map[string]ServeFieldConfig{
			"workloadType": {Label: "Workload Type", Type: "enum", Enum: []string{"app", "cert"}, Required: true},
			"image":        {Label: "Container Image"}, // not enum-typed — no rule
		},
	}}

	rules := c.EnumServeFieldRules()
	if len(rules) != 1 {
		t.Fatalf("got %d rules, want 1: %+v", len(rules), rules)
	}

	r := rules[0]
	if r.Field != "spec.workloadType" {
		t.Errorf("Field = %q, want %q", r.Field, "spec.workloadType")
	}
	if r.Operator != ConditionIn {
		t.Errorf("Operator = %q, want %q", r.Operator, ConditionIn)
	}
	if r.Value != "app,cert" {
		t.Errorf("Value = %q, want %q", r.Value, "app,cert")
	}
	if r.Message != "Workload Type must be one of: app, cert" {
		t.Errorf("Message = %q, want %q", r.Message, "Workload Type must be one of: app, cert")
	}
	if r.Action != ValidationActionDeny {
		t.Errorf("Action = %q, want %q", r.Action, ValidationActionDeny)
	}
}

// TestEnumServeFieldRules_ExistsGateIndependentOfRequired guards the exact bug
// caught while testing this against a live example: without an always-added
// exists gate, a non-required enum field that a CR simply omits would be
// denied anyway (ConditionIn fails closed on a missing field, same as
// exists) — silently turning an optional field into a required one.
// required: true must stay the only thing that makes a field
// mandatory; enum membership must only apply when a value is present.
func TestEnumServeFieldRules_ExistsGateIndependentOfRequired(t *testing.T) {
	appOnly := []Condition{{Field: "spec.workloadType", Equals: "app"}}

	c := &CRDEntry{Serve: &ServeConfig{
		Fields: map[string]ServeFieldConfig{
			// required + enum — exists gate is redundant with
			// RequiredServeFieldRules but must still be present.
			"workloadType": {Label: "Workload Type", Type: "enum", Enum: []string{"app", "cert"}, Required: true},
			// enum but NOT required, and carries its own When — both the
			// synthesized exists gate and the field's own When must be
			// present on the same rule.
			"environment": {Label: "Environment", Type: "enum", Enum: []string{"dev", "staging", "prod"}, When: appOnly},
		},
	}}

	rules := c.EnumServeFieldRules()

	requiredRule := findValidationRule(t, rules, "spec.workloadType")
	if len(requiredRule.When) != 1 || requiredRule.When[0].Field != "spec.workloadType" || requiredRule.When[0].Operator != ConditionExists {
		t.Errorf("workloadType rule.When = %+v, want a single synthesized exists gate on itself", requiredRule.When)
	}

	optionalRule := findValidationRule(t, rules, "spec.environment")
	if len(optionalRule.When) != 2 {
		t.Fatalf("environment rule.When = %+v, want 2 (synthesized exists gate + the field's own When)", optionalRule.When)
	}
	if optionalRule.When[0].Field != "spec.environment" || optionalRule.When[0].Operator != ConditionExists {
		t.Errorf("environment rule.When[0] = %+v, want the synthesized exists gate first", optionalRule.When[0])
	}
	if optionalRule.When[1].Field != "spec.workloadType" || optionalRule.When[1].Equals != "app" {
		t.Errorf("environment rule.When[1] = %+v, want the field's own When carried through", optionalRule.When[1])
	}
}

func TestEnumServeFieldRules_LabelsAndAnnotationsUseNotes(t *testing.T) {
	c := &CRDEntry{Serve: &ServeConfig{
		Labels: map[string]ServeFieldConfig{
			"tier": {Label: "Tier", Type: "enum", Enum: []string{"free", "pro", "enterprise"}},
		},
	}}

	rules := c.EnumServeFieldRules()
	if len(rules) != 1 {
		t.Fatalf("got %d rules, want 1: %+v", len(rules), rules)
	}

	field := `{{ getLabel . "tier" }}`
	r := findValidationRule(t, rules, field)
	if r.Value != "free,pro,enterprise" {
		t.Errorf("Value = %q, want %q", r.Value, "free,pro,enterprise")
	}
	if len(r.When) != 1 || r.When[0].Field != field || r.When[0].Operator != ConditionExists {
		t.Errorf("When = %+v, want a single synthesized exists gate using the same getLabel expression", r.When)
	}
	if r.Link != "tier" {
		t.Errorf("Link = %q, want %q", r.Link, "tier")
	}
}

func TestEnumServeFieldRules_LinkEmptyForSpecField(t *testing.T) {
	c := &CRDEntry{Serve: &ServeConfig{
		Fields: map[string]ServeFieldConfig{
			"workloadType": {Type: "enum", Enum: []string{"app", "cert"}},
		},
	}}

	rules := c.EnumServeFieldRules()
	r := findValidationRule(t, rules, "spec.workloadType")
	if r.Link != "" {
		t.Errorf("Link = %q, want empty — \"spec.workloadType\" is already a clean display name", r.Link)
	}
}

// Mutation Synthesis - ServeFieldMutationRules
func TestServeFieldMutationRules_NilCases(t *testing.T) {
	t.Run("nil CRDEntry", func(t *testing.T) {
		var c *CRDEntry
		if got := c.ServeFieldMutationRules(); got != nil {
			t.Errorf("got %v, want nil", got)
		}
	})
	t.Run("nil serve", func(t *testing.T) {
		c := &CRDEntry{}
		if got := c.ServeFieldMutationRules(); got != nil {
			t.Errorf("got %v, want nil", got)
		}
	})
	t.Run("serve with no default or override fields", func(t *testing.T) {
		c := &CRDEntry{Serve: &ServeConfig{
			Fields: map[string]ServeFieldConfig{
				"image": {Label: "Container Image"},
			},
			Labels: map[string]ServeFieldConfig{
				"image23": {Label: "Container Image"},
			},
			Annotations: map[string]ServeFieldConfig{
				"image": {Label: "Container Image"},
			},
		}}
		if got := c.ServeFieldMutationRules(); got != nil {
			t.Errorf("got %v, want nil — no mutation triggering field", got)
		}
	})
	t.Run("serve field with default", func(t *testing.T) {
		c := &CRDEntry{Serve: &ServeConfig{
			Fields: map[string]ServeFieldConfig{
				"image": {Label: "Container Image", Default: "myapp/v1"},
			},
		}}
		if got := c.ServeFieldMutationRules(); got != nil {
			t.Errorf("got %v, want nil — no mutation triggering for fields.default", got)
		}
	})
}

func TestServeFieldMutationRules_SpecFieldOverride(t *testing.T) {
	c := &CRDEntry{Serve: &ServeConfig{
		Fields: map[string]ServeFieldConfig{
			"team":  {Label: "Team", Override: "payments"},
			"image": {Label: "Container Image"}, // no override — no rule
		},
	}}

	rules := c.ServeFieldMutationRules()

	if len(rules) != 1 {
		t.Fatalf("got %d rules, want 1: %+v", len(rules), rules)
	}
	r := rules[0]
	if r.Field != "spec.team" {
		t.Errorf("Field = %q, want %q", r.Field, "spec.team")
	}
	if r.Default != nil {
		t.Errorf("Default = %q, want nil", r.Default)
	}
	if r.Override != "payments" {
		t.Errorf("Default = %q, want payments", r.Override)
	}
}

func TestServeFieldMutationRules_SpecFieldDefaultUsePath(t *testing.T) {
	c := &CRDEntry{Serve: &ServeConfig{
		Fields: map[string]ServeFieldConfig{
			"team":  {Override: "payments"},
			"image": {Override: "myorg/v1", Default: "myorg/latest", Path: "spec.deploy.image"},
		},
	}}

	rules := c.ServeFieldMutationRules()

	if len(rules) != 2 {
		t.Fatalf("got %d rules, want 2: %+v", len(rules), rules)
	}

	teamRule := MutationRuleFor(rules, "spec.team")
	if teamRule.Field != "spec.team" {
		t.Errorf("Field = %q, want %q", teamRule.Field, "spec.team")
	}
	if teamRule.Default != nil {
		t.Errorf("Default = %q, want nil", teamRule.Default)
	}
	if teamRule.Override != "payments" {
		t.Errorf("Override = %q, want payments", teamRule.Override)
	}

	imageRule := MutationRuleFor(rules, "spec.deploy.image")
	if imageRule.Field != "spec.deploy.image" {
		t.Errorf("Field = %q, want %q", imageRule.Field, "spec.deploy.image")
	}
	if imageRule.Default != nil {
		t.Errorf("Default = %q, want nil", imageRule.Default)
	}
	if imageRule.Override != "myorg/v1" {
		t.Errorf("Default = %q, want myorg/v1", imageRule.Override)
	}
}

func TestServeFieldMutationRules_SpecFieldInheritsWhenAndOr(t *testing.T) {
	appOnly := []Condition{{Field: "spec.workloadType", Equals: "app"}}
	certOrApp := []Condition{
		{Field: "spec.workloadType", Equals: "cert"},
		{Field: "spec.workloadType", Equals: "app"},
	}

	c := &CRDEntry{Serve: &ServeConfig{
		Fields: map[string]ServeFieldConfig{
			// A discriminator-routed field: only relevant when workloadType: app
			"repoURL": {Label: "Repository URL", Override: "myorg/payments.git", When: appOnly},
			// Or carries through the same way.
			"domain": {Label: "Domain", Override: "myorg.payments.test", Or: certOrApp},
			// A universal field with no condition at all.
			"team": {Label: "Team", Override: "payments"},
			// A skipped field: with default - already available in crd schema
			"ticketNo": {Label: "Ticket", Default: "PAY-123"},
		},
	}}

	rules := c.ServeFieldMutationRules()
	if len(rules) != 3 {
		t.Fatalf("got %d rules, want 3: %+v", len(rules), rules)
	}

	repoRule := findMutationRule(t, rules, "spec.repoURL")
	if len(repoRule.When) != 1 || repoRule.When[0].Field != "spec.workloadType" || repoRule.When[0].Equals != "app" {
		t.Errorf("repoURL rule.When = %+v, want the field's own When condition carried through", repoRule.When)
	}
	if len(repoRule.Or) != 0 {
		t.Errorf("repoURL rule.Or = %+v, want empty — the field declared When, not Or", repoRule.Or)
	}

	domainRule := findMutationRule(t, rules, "spec.domain")
	if len(domainRule.Or) != 2 {
		t.Errorf("domain rule.Or = %+v, want the field's own Or conditions carried through", domainRule.Or)
	}

	teamRule := findMutationRule(t, rules, "spec.team")
	if len(teamRule.When) != 0 || len(teamRule.Or) != 0 {
		t.Errorf("team rule When/Or = %+v/%+v, want both empty — the field declared neither", teamRule.When, teamRule.Or)
	}
}

func TestServeFieldMutationRules_LabelsAndAnnotationsUseNotes(t *testing.T) {
	c := &CRDEntry{Serve: &ServeConfig{
		Labels: map[string]ServeFieldConfig{
			"team":       {Override: "payments"},
			"department": {Default: "HR"},
		},
		Annotations: map[string]ServeFieldConfig{
			"platform.myorg.io/jira-ticket":     {Override: "PAY-123", Default: "to-be-ignored"},
			"platform.myorg.io/confluence-link": {Default: "myorg.atlassian.net"},
		},
	}}

	rules := c.ServeFieldMutationRules()
	if len(rules) != 4 {
		t.Fatalf("got %d rules, want 4: %+v", len(rules), rules)
	}

	// Labels
	labelRule1 := findMutationRule(t, rules, `{{ getLabel . "team" }}`)
	labelRule2 := findMutationRule(t, rules, `{{ getLabel . "department" }}`)
	if labelRule1.Override != "payments" || labelRule1.Default != nil {
		t.Errorf("label rule1 = %+v, want override as payments and default absent", labelRule1)
	}
	if labelRule2.Override != nil || labelRule2.Default != "HR" {
		t.Errorf("label rule2 = %+v, want override absent and default HR", labelRule2)
	}

	// Annotations
	annotationRule1 := findMutationRule(t, rules, `{{ getAnnotation . "platform.myorg.io/jira-ticket" }}`)
	annotationRule2 := findMutationRule(t, rules, `{{ getAnnotation . "platform.myorg.io/confluence-link" }}`)
	if annotationRule1.Override != "PAY-123" || annotationRule1.Default != nil {
		t.Errorf("annotation rule1 = %+v, want override as PAY-123 and default absent", annotationRule1)
	}
	if annotationRule2.Override != nil || annotationRule2.Default != "myorg.atlassian.net" {
		t.Errorf("annotation rule2 = %+v, want override absent and default myorg.atlassian.net", annotationRule2)
	}
}

func TestServeFieldMutationRules_LabelsAndAnnotationsInheritsWhenAndOr(t *testing.T) {
	appOnly := []Condition{{Field: "spec.workloadType", Equals: "app"}}
	certOrApp := []Condition{
		{Field: "spec.workloadType", Equals: "cert"},
		{Field: "spec.workloadType", Equals: "app"},
	}

	c := &CRDEntry{Serve: &ServeConfig{
		Labels: map[string]ServeFieldConfig{
			"team":       {Override: "payments", When: appOnly},
			"department": {Default: "HR", When: appOnly, Or: certOrApp},
		},
		Annotations: map[string]ServeFieldConfig{
			"platform.myorg.io/jira-ticket":     {Override: "PAY-123", Default: "to-be-ignored", When: appOnly},
			"platform.myorg.io/confluence-link": {Default: "myorg.atlassian.net", When: appOnly, Or: certOrApp},
			"platform.myorg.io/github-org":      {Default: "myorg.github.com"},
		},
	}}

	rules := c.ServeFieldMutationRules()
	if len(rules) != 5 {
		t.Fatalf("got %d rules, want 5: %+v", len(rules), rules)
	}

	// Labels
	teamRule := findMutationRule(t, rules, `{{ getLabel . "team" }}`)
	if len(teamRule.When) != 1 || teamRule.When[0].Field != "spec.workloadType" || teamRule.When[0].Equals != "app" {
		t.Errorf("team rule.When = %+v, want the field's own When condition carried through", teamRule.When)
	}
	if len(teamRule.Or) != 0 {
		t.Errorf("team rule.Or = %+v, want empty — the field declared When, not Or", teamRule.Or)
	}

	deptRule := findMutationRule(t, rules, `{{ getLabel . "department" }}`)
	if len(deptRule.When) != 1 || deptRule.When[0].Field != "spec.workloadType" || deptRule.When[0].Equals != "app" {
		t.Errorf("department rule.When = %+v, want the field's own When condition carried through", deptRule.When)
	}
	if len(deptRule.Or) != 2 {
		t.Errorf("department rule.Or = %+v, want the field's own Or conditions carried through", deptRule.Or)
	}

	// Annotations
	jiraRule := findMutationRule(t, rules, `{{ getAnnotation . "platform.myorg.io/jira-ticket" }}`)
	if len(jiraRule.When) != 1 || jiraRule.When[0].Field != "spec.workloadType" || jiraRule.When[0].Equals != "app" {
		t.Errorf("JIRA rule.When = %+v, want the field's own When condition carried through", jiraRule.When)
	}
	if len(jiraRule.Or) != 0 {
		t.Errorf("JIRA rule.Or = %+v, want empty — the field declared When, not Or", jiraRule.Or)
	}

	confluenceRule := findMutationRule(t, rules, `{{ getAnnotation . "platform.myorg.io/confluence-link" }}`)
	if len(confluenceRule.When) != 1 || confluenceRule.When[0].Field != "spec.workloadType" || confluenceRule.When[0].Equals != "app" {
		t.Errorf("confluence rule.When = %+v, want the field's own When condition carried through", confluenceRule.When)
	}
	if len(confluenceRule.Or) != 2 {
		t.Errorf("confluence rule.Or = %+v, want the field's own Or conditions carried through", confluenceRule.Or)
	}

	githubRule := findMutationRule(t, rules, `{{ getAnnotation . "platform.myorg.io/github-org" }}`)
	if len(githubRule.When) != 0 {
		t.Errorf("GitHub rule.Or = %+v, want empty — the field declared no conditions", githubRule.Or)
	}
	if len(githubRule.Or) != 0 {
		t.Errorf("GitHub rule.Or = %+v, want empty — the field declared no conditions", githubRule.Or)
	}
}
