package replicate

import "github.com/google/go-containerregistry/pkg/authn"

// Keychain maps registry hosts to credentials. Other hosts use anonymous access.
type Keychain map[string]authn.Authenticator

// Resolve implements authn.Keychain.
func (k Keychain) Resolve(r authn.Resource) (authn.Authenticator, error) {
	if a, ok := k[r.RegistryStr()]; ok {
		return a, nil
	}
	return authn.Anonymous, nil
}

// Add sets basic credentials for host. If username and password are both
// empty, Add does nothing and the host uses anonymous access.
func (k Keychain) Add(host, username, password string) {
	if username == "" && password == "" {
		return
	}
	k[host] = authn.FromConfig(authn.AuthConfig{Username: username, Password: password})
}
