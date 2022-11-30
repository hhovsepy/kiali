package kubernetes

import (
	"os"
	"os/exec"
	"time"
)

// Be careful with how you use this token. This is the Kiali Service Account token, not the user token.
// We need the Service Account token to access third-party in-cluster services (e.g. Grafana).

var DefaultServiceAccountPath = "/var/run/secrets/kubernetes.io/serviceaccount/token"

var KialiToken string
var tokenRead time.Time

func GetKialiToken() (string, error) {
	// TODO:refresh the token when it changes rather than after it expires
	if KialiToken == "" || shouldRefreshToken() {
		if remoteSecret, err := GetRemoteSecret(RemoteSecretData); err == nil {
			KialiToken = remoteSecret.Users[0].User.Token
		} else {
			token, err := os.ReadFile(DefaultServiceAccountPath)
			if err != nil {
				// TODO This is a change for a local setup. REMOVE
				cmd, err2 := exec.Command("kubectl", "exec", "deploy/kiali", "-n", "istio-system", "--", "cat", DefaultServiceAccountPath).Output()
				if err2 != nil {
					return "", err
				} else {
					KialiToken = string(cmd)
				}

			}
			KialiToken = string(token)
		}
		tokenRead = time.Now()
	}
	return KialiToken, nil
}

// Check if token expired based on the kubernetes configuration
func shouldRefreshToken() bool {

	timerDuration := time.Second * 60

	if time.Since(tokenRead) > timerDuration {
		return true
	} else {
		return false
	}
}
