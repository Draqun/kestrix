package credentials

import (
	"fmt"
	"strings"

	"kestrix/internal/domain"
)

const (
	TypeToken  = "token"
	TypeBasic  = "basic"
	TypeSSH    = "ssh"
	TypeSecret = "secret"
)

type normalized struct {
	Metadata domain.CredentialMetadata
	Secret   domain.Credential
}

func Normalize(ref string, input domain.CredentialInput) (normalized, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" || strings.TrimSpace(input.Name) == "" {
		return normalized{}, fmt.Errorf("credential ref and name are required")
	}
	kind := strings.ToLower(strings.TrimSpace(input.Type))
	if kind == "" {
		return normalized{}, fmt.Errorf("credential type is required")
	}
	secret := domain.Credential{Type: kind}
	switch kind {
	case TypeToken:
		if input.Token == "" {
			return normalized{}, fmt.Errorf("token credential requires token")
		}
		secret.Token, secret.Username = input.Token, input.Username
	case TypeBasic:
		if input.Username == "" || input.Password == "" {
			return normalized{}, fmt.Errorf("basic credential requires username and password")
		}
		secret.Username, secret.Password = input.Username, input.Password
	case TypeSSH:
		if input.PrivateKey == "" {
			return normalized{}, fmt.Errorf("ssh credential requires private_key")
		}
		secret.PrivateKey, secret.KnownHosts = input.PrivateKey, input.KnownHosts
	case TypeSecret:
		if input.Value == "" {
			return normalized{}, fmt.Errorf("secret credential requires value")
		}
		if input.Username != "" || input.Password != "" || input.Token != "" || input.PrivateKey != "" || input.KnownHosts != "" {
			return normalized{}, fmt.Errorf("secret credential must not set username, password, token, private_key, or known_hosts")
		}
		secret.Value = input.Value
	default:
		return normalized{}, fmt.Errorf("unsupported credential type %q", input.Type)
	}
	return normalized{Metadata: domain.CredentialMetadata{Ref: ref, Name: input.Name, Type: kind, Provider: input.Provider, Host: input.Host}, Secret: secret}, nil
}
