package routing

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

// ConfigSnapshot is a safe, read-only view of providers.json. Secrets are
// never decoded from provider entries. Group and model names are retained.
type ConfigSnapshot struct {
	Providers     []ProviderInfo `json:"providers"`
	Groups        []GroupInfo    `json:"groups"`
	ProviderOrder []string       `json:"providerOrder,omitempty"`
	GroupOrder    []string       `json:"groupOrder,omitempty"`
	NoAutoGroups  bool           `json:"noAutoGroups,omitempty"`
	ReadAt        time.Time      `json:"-"`
	ModifiedAt    time.Time      `json:"-"`
}

type ProviderInfo struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Icon     string   `json:"icon,omitempty"`
	Preset   string   `json:"preset,omitempty"`
	Routing  string   `json:"routing,omitempty"`
	Accounts int      `json:"accounts,omitempty"`
	Models   []string `json:"models,omitempty"`
	HasURL   bool     `json:"hasURL,omitempty"`
	HasKey   bool     `json:"hasKey,omitempty"`
	Quiet    bool     `json:"quiet,omitempty"`
	Disabled bool     `json:"disabled,omitempty"`
}

type GroupInfo struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Members    []string `json:"members"`
	Matched    []string `json:"matched,omitempty"`
	Match      []string `json:"match,omitempty"`
	Routing    string   `json:"routing,omitempty"`
	Affinity   string   `json:"affinity,omitempty"`
	Sink       bool     `json:"sink,omitempty"`
	Auto       bool     `json:"auto,omitempty"`
	Pick       string   `json:"pick,omitempty"`
	Off        []string `json:"off,omitempty"`
	Fast       []string `json:"fast,omitempty"`
	Levels     []string `json:"levels,omitempty"`
	Family     string   `json:"family,omitempty"`
	Hidden     bool     `json:"hidden,omitempty"`
	Disabled   bool     `json:"disabled,omitempty"`
	Rules      int      `json:"rules,omitempty"`
	FirstToken int      `json:"firstToken,omitempty"`
	Patience   int      `json:"patience,omitempty"`
	Classifier string   `json:"classifier,omitempty"`
	Effort     string   `json:"effort,omitempty"`
	Context    int      `json:"context,omitempty"`
}

// rawConfig deliberately names only non-secret fields. Provider's key, keys,
// headers, tokens and search configurations are not represented, so they
// cannot accidentally be logged or shown.
type rawConfig struct {
	Providers    []rawProvider `json:"providers"`
	Groups       []rawGroup    `json:"groups"`
	Order        []string      `json:"order"`
	GroupOrder   []string      `json:"groupOrder"`
	NoAutoGroups bool          `json:"noAutoGroups"`
}

type rawProvider struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Icon      string          `json:"icon,omitempty"`
	Preset    string          `json:"preset,omitempty"`
	Routing   string          `json:"routing,omitempty"`
	Key       string          `json:"key,omitempty"`
	KeyName   string          `json:"keyName,omitempty"`
	Account   json.RawMessage `json:"account,omitempty"`
	Quiet     bool            `json:"quiet,omitempty"`
	Disabled  bool            `json:"disabled,omitempty"`
	Chat      string          `json:"chat,omitempty"`
	Responses string          `json:"responses,omitempty"`
	Anthropic string          `json:"anthropic,omitempty"`
	Gemini    string          `json:"gemini,omitempty"`
	Models    []string        `json:"models,omitempty"`
}

type rawGroup struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Members    []string          `json:"members"`
	Matched    []string          `json:"matched,omitempty"`
	Match      []string          `json:"match,omitempty"`
	Routing    string            `json:"routing,omitempty"`
	Affinity   string            `json:"affinity,omitempty"`
	Sink       bool              `json:"sink,omitempty"`
	Auto       bool              `json:"auto,omitempty"`
	Pick       string            `json:"pick,omitempty"`
	Off        []string          `json:"off,omitempty"`
	Fast       []string          `json:"fast,omitempty"`
	Levels     []string          `json:"levels,omitempty"`
	Family     string            `json:"family,omitempty"`
	Hidden     bool              `json:"hidden,omitempty"`
	Disabled   bool              `json:"disabled,omitempty"`
	Rules      []json.RawMessage `json:"rules,omitempty"`
	FirstToken int               `json:"firstToken,omitempty"`
	Patience   int               `json:"patience,omitempty"`
	Classifier string            `json:"classifier,omitempty"`
	Effort     string            `json:"effort,omitempty"`
	Context    int               `json:"context,omitempty"`
}

// ConfigReader reads <magpie-dir>/providers.json as a safe snapshot.
type ConfigReader struct {
	Path string
}

func NewConfigReader(path string) ConfigReader { return ConfigReader{Path: path} }

// Read reads and parses providers.json. It never follows an expected write
// path and does not mutate the file. A symlink is accepted for reading.
func (r ConfigReader) Read() (ConfigSnapshot, error) {
	b, err := os.ReadFile(r.Path)
	if err != nil {
		return ConfigSnapshot{}, err
	}
	var raw rawConfig
	dec := json.NewDecoder(strings.NewReader(string(b)))
	if err := dec.Decode(&raw); err != nil {
		return ConfigSnapshot{}, fmt.Errorf("decode providers.json: %w", err)
	}
	out := ConfigSnapshot{
		ProviderOrder: append([]string(nil), raw.Order...),
		GroupOrder:    append([]string(nil), raw.GroupOrder...),
		NoAutoGroups:  raw.NoAutoGroups,
		ReadAt:        time.Now(),
	}
	if fi, err := os.Stat(r.Path); err == nil {
		out.ModifiedAt = fi.ModTime()
	}
	for _, p := range raw.Providers {
		if p.ID == "" {
			continue
		}
		info := ProviderInfo{
			ID: p.ID, Name: p.Name, Icon: p.Icon, Preset: p.Preset,
			Routing: p.Routing, Models: append([]string(nil), p.Models...),
			HasURL: p.Chat != "" || p.Responses != "" || p.Anthropic != "" || p.Gemini != "",
			HasKey: p.Key != "" || p.KeyName != "" || len(p.Account) > 2,
			Quiet:  p.Quiet, Disabled: p.Disabled,
		}
		out.Providers = append(out.Providers, info)
	}
	for _, g := range raw.Groups {
		if g.ID == "" {
			continue
		}
		out.Groups = append(out.Groups, GroupInfo{
			ID: g.ID, Name: g.Name, Members: append([]string(nil), g.Members...),
			Matched: append([]string(nil), g.Matched...),
			Match:   append([]string(nil), g.Match...), Routing: g.Routing,
			Affinity: g.Affinity, Sink: g.Sink, Auto: g.Auto, Pick: g.Pick,
			Off: append([]string(nil), g.Off...), Fast: append([]string(nil), g.Fast...),
			Levels: append([]string(nil), g.Levels...), Family: g.Family, Hidden: g.Hidden,
			Disabled: g.Disabled, Rules: len(g.Rules), FirstToken: g.FirstToken,
			Patience: g.Patience, Classifier: g.Classifier, Effort: g.Effort,
			Context: g.Context,
		})
	}
	sort.SliceStable(out.Groups, func(i, j int) bool {
		// Keep explicit group order first, then the file's order.
		ri, rj := groupRank(out.GroupOrder, out.Groups[i].ID), groupRank(out.GroupOrder, out.Groups[j].ID)
		if ri != rj {
			return ri < rj
		}
		return out.Groups[i].ID < out.Groups[j].ID
	})
	populateAutoGroups(&out, raw)
	return out, nil
}

// populateAutoGroups marks groups Magpie derives from one model served by
// several providers. A stored "auto-…" group can be the user's changed copy,
// so the prefix alone is not proof. A group carrying only a derived shape
// (no rules and no match patterns) is still labeled as automatic only when
// Magpie's naming convention agrees; the UI also keeps the persisted flag.
func populateAutoGroups(out *ConfigSnapshot, raw rawConfig) {
	explicit := make(map[string]bool, len(raw.Groups))
	for _, g := range raw.Groups {
		explicit[strings.ToLower(g.ID)] = g.Auto
	}
	for i := range out.Groups {
		out.Groups[i].Auto = out.Groups[i].Auto ||
			explicit[strings.ToLower(out.Groups[i].ID)] ||
			(strings.HasPrefix(out.Groups[i].ID, "auto-") &&
				out.Groups[i].Rules == 0 && len(out.Groups[i].Match) == 0)
	}
}

func groupRank(order []string, id string) int {
	for i, x := range order {
		if x == id {
			return i
		}
	}
	return len(order) + 1000
}

// GroupName returns a display name, falling back to the id.
func GroupName(g GroupInfo) string {
	if strings.TrimSpace(g.Name) != "" {
		return g.Name
	}
	return g.ID
}

// FindProvider looks up a provider by id.
func (c ConfigSnapshot) FindProvider(id string) (ProviderInfo, bool) {
	for _, p := range c.Providers {
		if strings.EqualFold(p.ID, id) {
			return p, true
		}
	}
	return ProviderInfo{}, false
}

// FindGroup looks up a group by id.
func (c ConfigSnapshot) FindGroup(id string) (GroupInfo, bool) {
	for _, g := range c.Groups {
		if strings.EqualFold(g.ID, id) {
			return g, true
		}
	}
	return GroupInfo{}, false
}

// ErrNoConfig is returned when providers.json is absent.
var ErrNoConfig = errors.New("providers.json not found")
