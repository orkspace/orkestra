package jobs

import (
	"fmt"

	"github.com/orkspace/orkestra/domain"
	"github.com/orkspace/orkestra/pkg/resources/shared"
)

// BuildFromIntent converts a flat intent fields map into a full Job object map
// suitable for application via SSA.
// Required fields: name, image.
func BuildFromIntent(fields map[string]interface{}, owner domain.Object) (map[string]interface{}, error) {
	var f shared.JobIntentFields
	if err := shared.DecodeFields(fields, &f); err != nil {
		return nil, fmt.Errorf("job.BuildFromIntent: %w", err)
	}
	if f.Name == "" {
		return nil, fmt.Errorf("job.BuildFromIntent: name is required")
	}
	if f.Image == "" {
		return nil, fmt.Errorf("job.BuildFromIntent: image is required")
	}

	backoffLimit := f.BackoffLimit
	if backoffLimit == 0 {
		backoffLimit = 3
	}

	namespace := shared.ResolveNamespace(owner, "")

	spec := ResolvedJobSpec{
		Name:         f.Name,
		Namespace:    namespace,
		Image:        f.Image,
		Command:      f.Command,
		Args:         f.Args,
		BackoffLimit: backoffLimit,
	}

	obj := buildJob(owner, spec, namespace, true)

	rawMap, err := shared.ToObjectMap(obj, "batch/v1", "Job")
	if err != nil {
		return nil, fmt.Errorf("job.BuildFromIntent: convert to unstructured: %w", err)
	}
	if err := shared.ApplyMetaOverrides(rawMap, f.Labels, f.Annotations); err != nil {
		return nil, fmt.Errorf("job.BuildFromIntent: %w", err)
	}
	return rawMap, nil
}
