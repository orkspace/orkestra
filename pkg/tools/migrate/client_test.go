package migrate

import (
	"go/parser"
	"go/token"
	"testing"
)

func TestClientFieldKey(t *testing.T) {
	tests := []struct {
		name         string
		source       string
		receiverType string
		wantKey      string
		wantFound    bool
	}{
		{
			name: "default import named field",
			source: `
				import "sigs.k8s.io/controller-runtime/pkg/client"

				type Reconciler struct {
					k8sClient client.Client
				}
			`,
			receiverType: "Reconciler",
			wantKey:      "k8sClient",
			wantFound:    true,
		},
		{
			name: "aliased import",
			source: `
				import ctrl "sigs.k8s.io/controller-runtime/pkg/client"

				type Reconciler struct {
					k8sClient ctrl.Client
				}
			`,
			receiverType: "Reconciler",
			wantKey:      "k8sClient",
			wantFound:    true,
		},
		{
			name: "arbitrary import alias",
			source: `
				import crclient "sigs.k8s.io/controller-runtime/pkg/client"

				type Reconciler struct {
					k8sClient crclient.Client
				}
			`,
			receiverType: "Reconciler",
			wantKey:      "k8sClient",
			wantFound:    true,
		},
		{
			name: "dot import named field",
			source: `
				import . "sigs.k8s.io/controller-runtime/pkg/client"

				type Reconciler struct {
					k8sClient Client
				}
			`,
			receiverType: "Reconciler",
			wantKey:      "k8sClient",
			wantFound:    true,
		},
		{
			name: "embedded default import",
			source: `
				import "sigs.k8s.io/controller-runtime/pkg/client"

				type Reconciler struct {
					client.Client
				}
			`,
			receiverType: "Reconciler",
			wantKey:      "Client",
			wantFound:    true,
		},
		{
			name: "embedded aliased import",
			source: `
				import ctrl "sigs.k8s.io/controller-runtime/pkg/client"

				type Reconciler struct {
					ctrl.Client
				}
			`,
			receiverType: "Reconciler",
			wantKey:      "Client",
			wantFound:    true,
		},
		{
			name: "embedded pointer",
			source: `
				import crclient "sigs.k8s.io/controller-runtime/pkg/client"

				type Reconciler struct {
					*crclient.Client
				}
			`,
			receiverType: "Reconciler",
			wantKey:      "Client",
			wantFound:    true,
		},
		{
			name: "unrelated Client type",
			source: `
				import foo "example.com/foo"

				type Reconciler struct {
					k8sClient foo.Client
				}
			`,
			receiverType: "Reconciler",
			wantKey:      "",
			wantFound:    false,
		},
		{
			name: "missing import",
			source: `
				type Reconciler struct {
					k8sClient client.Client
				}
			`,
			receiverType: "Reconciler",
			wantKey:      "",
			wantFound:    false,
		},
		{
			name: "pointer named field",
			source: `
		import "sigs.k8s.io/controller-runtime/pkg/client"

		type Reconciler struct {
			k8sClient *client.Client
		}
	`,
			receiverType: "Reconciler",
			wantKey:      "k8sClient",
			wantFound:    true,
		},
		{
			name: "parenthesized client type",
			source: `
		import "sigs.k8s.io/controller-runtime/pkg/client"

		type Reconciler struct {
			k8sClient (client.Client)
		}
	`,
			receiverType: "Reconciler",
			wantKey:      "k8sClient",
			wantFound:    true,
		},
		{
			name: "parenthesized pointer client type",
			source: `
		import ctrl "sigs.k8s.io/controller-runtime/pkg/client"

		type Reconciler struct {
			k8sClient (*ctrl.Client)
		}
	`,
			receiverType: "Reconciler",
			wantKey:      "k8sClient",
			wantFound:    true,
		},
		{
			name: "client field is not first",
			source: `
		import "sigs.k8s.io/controller-runtime/pkg/client"

		type Reconciler struct {
			Name      string
			Namespace string
			k8sClient client.Client
		}
	`,
			receiverType: "Reconciler",
			wantKey:      "k8sClient",
			wantFound:    true,
		},
		{
			name: "named field called Client",
			source: `
		import "sigs.k8s.io/controller-runtime/pkg/client"

		type Reconciler struct {
			Client client.Client
		}
	`,
			receiverType: "Reconciler",
			wantKey:      "Client",
			wantFound:    true,
		},
		{
			name: "dot import pointer",
			source: `
		import . "sigs.k8s.io/controller-runtime/pkg/client"

		type Reconciler struct {
			k8sClient *Client
		}
	`,
			receiverType: "Reconciler",
			wantKey:      "k8sClient",
			wantFound:    true,
		},
		{
			name: "blank import does not match",
			source: `
		import _ "sigs.k8s.io/controller-runtime/pkg/client"

		type Reconciler struct {
			k8sClient Client
		}
	`,
			receiverType: "Reconciler",
			wantKey:      "",
			wantFound:    false,
		},
		{
			name: "unrelated package with Client type",
			source: `
		import (
			client "example.com/other/client"
			ctrl "sigs.k8s.io/controller-runtime/pkg/client"
		)

		type Reconciler struct {
			k8sClient client.Client
			realClient ctrl.Client
		}
	`,
			receiverType: "Reconciler",
			wantKey:      "realClient",
			wantFound:    true,
		},
		{
			name: "unrelated Client selector with controller-runtime alias",
			source: `
		import (
			foo "example.com/foo"
			crclient "sigs.k8s.io/controller-runtime/pkg/client"
		)

		type Reconciler struct {
			fooClient foo.Client
			k8sClient crclient.Client
		}
	`,
			receiverType: "Reconciler",
			wantKey:      "k8sClient",
			wantFound:    true,
		},
		{
			name: "receiver is not a struct",
			source: `
		import "sigs.k8s.io/controller-runtime/pkg/client"

		type Reconciler string
	`,
			receiverType: "Reconciler",
			wantKey:      "",
			wantFound:    false,
		},
		{
			name: "receiver does not exist",
			source: `
		import "sigs.k8s.io/controller-runtime/pkg/client"

		type Other struct {
			k8sClient client.Client
		}
	`,
			receiverType: "Reconciler",
			wantKey:      "",
			wantFound:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := "package migrate\n" + tt.source

			fset := token.NewFileSet()
			f, err := parser.ParseFile(
				fset,
				"",
				source,
				parser.ParseComments,
			)
			if err != nil {
				t.Fatalf("could not parse source: %v", err)
			}

			gotKey, gotFound := clientFieldKey(f, tt.receiverType)
			if gotKey != tt.wantKey || gotFound != tt.wantFound {
				t.Errorf(
					"clientFieldKey() = (%q, %v), want (%q, %v)",
					gotKey,
					gotFound,
					tt.wantKey,
					tt.wantFound,
				)
			}
		})
	}
}
