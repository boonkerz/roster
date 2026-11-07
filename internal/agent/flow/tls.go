package flow

import (
	"crypto/tls"
	"net/http"
)

func insecureTransport() http.RoundTripper {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // bewusst wählbar (Selbstsignierte interne APIs)
	return t
}
