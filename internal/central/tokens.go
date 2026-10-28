package central

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/root-lr/observaboard/internal/fsutil"
	"github.com/root-lr/observaboard/internal/project"
	"github.com/root-lr/observaboard/internal/thirdparty/bcrypt"
	"github.com/root-lr/observaboard/internal/thirdparty/yaml"
)

// Token authorises one agent to push data. Only the bcrypt hash is stored.
type Token struct {
	Name    string `yaml:"name"`
	Hash    string `yaml:"hash"`
	Created string `yaml:"created"`
}

// bcryptCost is deliberately low: tokens are 256-bit random secrets, so the
// hash cost adds no security, while Caddy verifies it on every push request.
const bcryptCost = bcrypt.MinCost

const tokensFile = "tokens.yaml"

// Tokens loads tokens.yaml.
func (d Dir) Tokens() ([]Token, error) {
	data, err := os.ReadFile(filepath.Join(d.Path, tokensFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var t []Token
	if err := yaml.Unmarshal(data, &t); err != nil {
		return nil, fmt.Errorf("%s : %w", tokensFile, err)
	}
	return t, nil
}

func (d Dir) saveTokens(t []Token) error {
	sort.Slice(t, func(i, j int) bool { return t[i].Name < t[j].Name })
	data, err := yaml.Marshal(t)
	if err != nil {
		return err
	}
	header := "# Tokens des agents (hachés). Gérer avec : observa token create|list|revoke\n"
	_, err = fsutil.WriteFile(filepath.Join(d.Path, tokensFile), append([]byte(header), data...), 0o600)
	return err
}

// CreateToken creates a token for an agent and returns it in clear text
// ("<name>.<secret>"); it is shown once and never stored.
func (d Dir) CreateToken(name string) (string, error) {
	if !project.SlugRE.MatchString(name) {
		return "", fmt.Errorf("nom d'agent %q invalide (minuscules, chiffres et tirets, ex. srv-a)", name)
	}
	tokens, err := d.Tokens()
	if err != nil {
		return "", err
	}
	for _, t := range tokens {
		if t.Name == name {
			return "", fmt.Errorf("un token existe déjà pour %s (révoquez-le d'abord : observa token revoke %s)", name, name)
		}
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	secret := base64.RawURLEncoding.EncodeToString(raw)
	hash, err := bcrypt.GenerateFromPassword([]byte(secret), bcryptCost)
	if err != nil {
		return "", err
	}
	tokens = append(tokens, Token{Name: name, Hash: string(hash), Created: time.Now().UTC().Format(time.RFC3339)})
	if err := d.saveTokens(tokens); err != nil {
		return "", err
	}
	return name + "." + secret, nil
}

// RevokeToken deletes the token of an agent.
func (d Dir) RevokeToken(name string) error {
	tokens, err := d.Tokens()
	if err != nil {
		return err
	}
	var kept []Token
	found := false
	for _, t := range tokens {
		if t.Name == name {
			found = true
			continue
		}
		kept = append(kept, t)
	}
	if !found {
		return fmt.Errorf("aucun token pour %s", name)
	}
	return d.saveTokens(kept)
}

// SplitToken splits "<name>.<secret>" into its parts.
func SplitToken(tok string) (name, secret string, err error) {
	tok = strings.TrimSpace(tok)
	i := strings.Index(tok, ".")
	if i <= 0 || i == len(tok)-1 {
		return "", "", errors.New("token invalide : format attendu <agent>.<secret> (affiché par observa token create)")
	}
	name, secret = tok[:i], tok[i+1:]
	if !project.SlugRE.MatchString(name) {
		return "", "", fmt.Errorf("token invalide : nom d'agent %q", name)
	}
	return name, secret, nil
}
