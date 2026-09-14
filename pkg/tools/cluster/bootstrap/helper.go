package bootstrap

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/orkspace/orkestra/pkg/utils"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

var readLocal = utils.ReadLocal

func waitForToken(ctx context.Context, cs kubernetes.Interface, ns, name string, log func(string)) (string, error) {
	for i := 0; i < 10; i++ {
		s, err := cs.CoreV1().Secrets(ns).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return "", fmt.Errorf("reading token Secret: %w", err)
		}
		if token := strings.TrimSpace(string(s.Data["token"])); token != "" {
			return token, nil
		}
		log("waiting for token controller to populate...")
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
	return "", fmt.Errorf("token not populated after 30s — check the token controller is running")
}
