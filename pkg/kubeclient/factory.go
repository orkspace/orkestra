package kubeclient

import (
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/rest"
)

// CRDInfo defines what a CRD is
type CRDInfo struct {
	Kind                 string                  // Required by Registry
	Group                string                  // Required if GroupVersion is not specified
	Version              string                  // Required if GroupVersion is not specified
	GroupVersion         *schema.GroupVersion    // Optional (can be used if Group and Version are not specified)
	GroupVersionKind     schema.GroupVersionKind //	Useful for some manipulations
	GroupVersionResource schema.GroupResource
	Plural               string
	Namespaced           bool // Required for cluster-scoped resources
	APIPath              string
	Namespace            string
	ForceConflict        *bool
}

// SharedClientFactory provides a simple way to build clients from config
func (k *Kubeclient) SharedClientFactory(apiPath, group, version string) (*rest.RESTClient, error) {
	// Build restclient
	if k.restConfig == nil {
		return nil, fmt.Errorf("kubeclient not started — restConfig is nil")
	}

	// Copy to not mutate the base config
	cfg := rest.CopyConfig(k.restConfig)
	cfg.GroupVersion = &schema.GroupVersion{
		Group:   group,
		Version: version,
	}
	cfg.APIPath = apiPath
	cfg.ContentType = runtime.ContentTypeJSON
	cfg.NegotiatedSerializer = serializer.NewCodecFactory(k.scheme).WithoutConversion()

	return rest.RESTClientFor(cfg)
}

func (k *Kubeclient) RuntimeParameterCodec() runtime.ParameterCodec {
	return runtime.NewParameterCodec(k.Scheme())
}
