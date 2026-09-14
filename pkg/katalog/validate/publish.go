package validate

import (
	"fmt"

	"github.com/orkspace/orkestra/pkg/katalog"
	orktypes "github.com/orkspace/orkestra/pkg/types"
)

// validatePublish checks the publish: block for structural correctness.
// Semantic enforcement (does intent.yaml exist, is cosign installed) happens
// at push time — the katalog may be authored without cosign or an intent file.
func (e *executor) validatePublish() error {
	pub := e.k.Publish
	if pub == nil {
		return nil
	}

	if err := validateSigningConfig(pub); err != nil {
		return err
	}

	if err := validatePublishTests(e.k, pub); err != nil {
		return err
	}

	return nil
}

func validateSigningConfig(pub *orktypes.PublishConfig) error {
	if !pub.HasExpectedIdentities() {
		return nil
	}
	for i, id := range pub.ExpectedIdentities() {
		if id == "" {
			return fmt.Errorf("%s publish.signing.expectedIdentities[%d]: identity must not be empty", failureMark(), i)
		}
	}
	return nil
}

func validatePublishTests(k *katalog.Katalog, pub *orktypes.PublishConfig) error {
	if !pub.TestsConfig().IntentEnabled() {
		return nil
	}

	// intent: true requires gateway.api — intent play targets the serve endpoint.
	if !k.IsGatewayEnabled() || !k.Gateway.HasAPI() {
		return fmt.Errorf("%s publish.tests.intent: true requires gateway.api.enabled: true — intent play targets the serve endpoint", failureMark())
	}

	// intent: true requires intent.yaml or intent.json to be present.
	if !k.HasIntentFiles() {
		return fmt.Errorf("%spublish.tests.intent: true requires intent.yaml or intent.json in the pattern directory", failureMark())
	}

	return nil
}
