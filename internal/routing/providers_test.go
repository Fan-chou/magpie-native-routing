package routing

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigReaderNeverRetainsSecrets(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "providers.json")
	body := `{
	  "providers": [
	    {"id":"safe","name":"Safe Relay","key":"sk-super-secret","accessKeyID":"AKIA-SECRET","secretAccessKey":"SECRET2","headers":{"Authorization":"Bearer TOPSECRET"},"account":{"token":"ACCOUNT-SECRET"},"chat":"https://example.test/v1","models":["m1"],"routing":"order"},
	    {"id":"off","name":"Off","quiet":true}
	  ],
	  "groups": [
	    {"id":"g1","name":"Group One","members":["safe/m1"],"match":["safe/*"],"routing":"order","rules":[{"when":["x"],"use":"safe/m1"}],"auto":false}
	  ],
	  "order":["safe","off"],
	  "groupOrder":["g1"],
	  "noAutoGroups":true
	}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := NewConfigReader(path).Read()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Providers) != 2 || cfg.Providers[0].ID != "safe" || !cfg.Providers[0].HasKey || !cfg.Providers[0].HasURL {
		t.Fatalf("providers = %+v", cfg.Providers)
	}
	if len(cfg.Groups) != 1 || cfg.Groups[0].Rules != 1 || cfg.Groups[0].Members[0] != "safe/m1" {
		t.Fatalf("groups = %+v", cfg.Groups)
	}
	all := cfgString(cfg)
	for _, secret := range []string{"sk-super-secret", "AKIA-SECRET", "SECRET2", "TOPSECRET", "ACCOUNT-SECRET"} {
		if strings.Contains(all, secret) {
			t.Fatalf("snapshot leaked %q: %s", secret, all)
		}
	}
}

func cfgString(c ConfigSnapshot) string {
	var b strings.Builder
	for _, p := range c.Providers {
		b.WriteString(p.ID)
		b.WriteString(p.Name)
		b.WriteString(p.Routing)
		b.WriteString(strings.Join(p.Models, ","))
	}
	for _, g := range c.Groups {
		b.WriteString(g.ID)
		b.WriteString(g.Name)
		b.WriteString(strings.Join(g.Members, ","))
		b.WriteString(strings.Join(g.Match, ","))
	}
	return b.String()
}

func TestConfigReaderMalformed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "providers.json")
	if err := os.WriteFile(path, []byte(`{"providers":`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewConfigReader(path).Read(); err == nil {
		t.Fatal("malformed config accepted")
	}
}
