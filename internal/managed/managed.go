// Package managed decides whether a container is owned by another tool and
// therefore must never be upgraded by dmanager.
package managed

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Label is the container label that marks a container as externally managed.
// Any non-empty value counts; the value names the managing tool (for example
// "nixos") and is shown in the UI.
const Label = "dmanager.managed-by"

// Selector matches a label by key, and by value when HasValue is set.
type Selector struct {
	Key      string
	Value    string
	HasValue bool
}

// String renders the selector in its configuration form.
func (s Selector) String() string {
	if s.HasValue {
		return s.Key + "=" + s.Value
	}
	return s.Key
}

// Matches reports whether labels satisfy the selector.
func (s Selector) Matches(labels map[string]string) bool {
	v, ok := labels[s.Key]
	if !ok {
		return false
	}
	return !s.HasValue || v == s.Value
}

// ParseSelector parses "key=value" or a bare "key" (label present).
func ParseSelector(raw string) (Selector, error) {
	raw = strings.TrimSpace(raw)
	key, value, hasValue := strings.Cut(raw, "=")
	key = strings.TrimSpace(key)
	if key == "" {
		return Selector{}, fmt.Errorf("label selector %q has an empty key", raw)
	}
	if !hasValue {
		return Selector{Key: key}, nil
	}
	return Selector{Key: key, Value: strings.TrimSpace(value), HasValue: true}, nil
}

// ParseSelectors parses a list of selectors, skipping blank entries.
func ParseSelectors(raw []string) ([]Selector, error) {
	out := make([]Selector, 0, len(raw))
	for _, r := range raw {
		if strings.TrimSpace(r) == "" {
			continue
		}
		s, err := ParseSelector(r)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

// Policy is the set of extra selectors from the configuration.
type Policy struct {
	Selectors []Selector
}

// NewPolicy builds a Policy from configured selector strings. Invalid entries
// are rejected by config validation; here they are skipped.
func NewPolicy(raw []string) Policy {
	sel, _ := ParseSelectors(raw)
	return Policy{Selectors: sel}
}

// Check reports whether a container with these labels is externally managed
// and, if so, by whom: the value of the dmanager label, or the first matching
// configured selector in its configuration form.
func (p Policy) Check(labels map[string]string) (managedBy string, ok bool) {
	if v := strings.TrimSpace(labels[Label]); v != "" {
		return v, true
	}
	for _, s := range p.Selectors {
		if s.Matches(labels) {
			return s.String(), true
		}
	}
	return "", false
}

// EncodeLabels serialises labels for storage.
func EncodeLabels(labels map[string]string) string {
	if len(labels) == 0 {
		return "{}"
	}
	b, err := json.Marshal(labels)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// DecodeLabels parses stored labels; malformed input yields no labels.
func DecodeLabels(s string) map[string]string {
	var m map[string]string
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return nil
	}
	return m
}
