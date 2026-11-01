package rules

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type RuleDraft struct {
	Name    string           `json:"name"`
	Enabled bool             `json:"enabled"`
	Filter  map[string]any   `json:"filter"`
	Actions []map[string]any `json:"actions"`
}

type RuleModel struct {
	Rules []RuleDraft `json:"rules"`
}

type ParsedRule struct {
	Name        string           `json:"name"`
	Filter      map[string]any   `json:"filter"`
	Actions     []map[string]any `json:"actions"`
	FilterKind  string           `json:"filterKind"`
	ActionKinds []string         `json:"actionKinds"`
}

const DefaultYAML = `rules:
  - name: 默认规则（不影响流量）
    filter:
      typeKind: AnyFilter
    actions:
      - typeKind: NoOpAction
`

func DefaultHash() string { return HashYAML(DefaultYAML) }

func HashYAML(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// Render produces fluxzy's native rules.yaml from the visual model.
func Render(model RuleModel) (string, error) {
	enabled := make([]RuleDraft, 0, len(model.Rules))
	for _, r := range model.Rules {
		if r.Enabled {
			enabled = append(enabled, r)
		}
	}
	if len(enabled) == 0 {
		return DefaultYAML, nil
	}

	root := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	rulesNode := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	root.Content = append(root.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "rules"},
		rulesNode)

	for _, r := range enabled {
		filter := expandDotted(r.Filter)
		filter = normalizeMap(filter)
		filter = removeEmpty(filter).(map[string]any)
		filterKind, _ := filter["typeKind"].(string)
		if filterKind == "" {
			return "", fmt.Errorf("规则 %q 缺少 filter.typeKind", r.Name)
		}
		if err := validateRequired(filterKind, filter, true); err != nil {
			return "", err
		}

		actions := make([]any, 0, len(r.Actions))
		for _, a := range r.Actions {
			action := expandDotted(a)
			action = normalizeMap(action)
			action = removeEmpty(action).(map[string]any)
			kind, _ := action["typeKind"].(string)
			if kind == "" {
				return "", fmt.Errorf("规则 %q 存在缺少 typeKind 的 action", r.Name)
			}
			if kind == "ApplyTagAction" {
				ensureTagIdentifier(action)
			}
			if err := validateRequired(kind, action, false); err != nil {
				return "", err
			}
			actions = append(actions, action)
		}
		if len(actions) == 0 {
			return "", fmt.Errorf("规则 %q 至少需要一个 action", r.Name)
		}

		ruleNode := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		if strings.TrimSpace(r.Name) != "" {
			ruleNode.Content = append(ruleNode.Content,
				&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "name"},
				&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: r.Name})
		}
		ruleNode.Content = append(ruleNode.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "filter"},
			valueNode(filter),
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "actions"},
			valueNode(actions))
		rulesNode.Content = append(rulesNode.Content, ruleNode)
	}

	var b strings.Builder
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(root); err != nil {
		return "", err
	}
	_ = enc.Close()
	return strings.TrimRight(b.String(), "\n") + "\n", nil
}

// Parse converts an existing rules.yaml into the visual model. It is
// best-effort: unknown fields are preserved in the maps so switching to the
// YAML editor and back does not silently drop them.
func Parse(data []byte) (RuleModel, error) {
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return RuleModel{}, err
	}
	rulesRaw, ok := raw["rules"]
	if !ok {
		return RuleModel{}, fmt.Errorf("缺少顶层 rules")
	}
	list, ok := asSlice(rulesRaw)
	if !ok {
		return RuleModel{}, fmt.Errorf("rules 必须是数组")
	}
	model := RuleModel{Rules: make([]RuleDraft, 0, len(list))}
	for _, item := range list {
		m, ok := asMap(item)
		if !ok {
			continue
		}
		r := RuleDraft{Enabled: true}
		if v, ok := m["name"].(string); ok {
			r.Name = v
		}
		r.Filter, _ = asMap(m["filter"])
		if r.Filter == nil {
			r.Filter = map[string]any{}
		}
		if v, ok := m["actions"]; ok {
			if arr, ok := asSlice(v); ok {
				for _, a := range arr {
					if am, ok := asMap(a); ok {
						r.Actions = append(r.Actions, normalizeMap(am))
					}
				}
			}
		} else if v, ok := m["action"]; ok {
			if am, ok := asMap(v); ok {
				r.Actions = append(r.Actions, normalizeMap(am))
			}
		}
		r.Filter = normalizeMap(r.Filter)
		model.Rules = append(model.Rules, r)
	}
	return model, nil
}

func Summaries(data []byte) ([]ParsedRule, error) {
	model, err := Parse(data)
	if err != nil {
		return nil, err
	}
	out := make([]ParsedRule, 0, len(model.Rules))
	for _, r := range model.Rules {
		p := ParsedRule{Name: r.Name, Filter: r.Filter, Actions: r.Actions}
		p.FilterKind, _ = r.Filter["typeKind"].(string)
		for _, a := range r.Actions {
			k, _ := a["typeKind"].(string)
			p.ActionKinds = append(p.ActionKinds, k)
		}
		out = append(out, p)
	}
	return out, nil
}

func ValidateStructure(data []byte) (errors []string, warnings []string) {
	model, err := Parse(data)
	if err != nil {
		return []string{err.Error()}, nil
	}
	if len(model.Rules) == 0 {
		return []string{"rules 不能为空"}, nil
	}
	schema := BuildSchema()
	for i, r := range model.Rules {
		label := r.Name
		if label == "" {
			label = fmt.Sprintf("规则 #%d", i+1)
		}
		kind, _ := r.Filter["typeKind"].(string)
		if kind == "" {
			errors = append(errors, fmt.Sprintf("%s：缺少 filter.typeKind", label))
		} else if _, ok := schema.FindFilter(kind); !ok {
			warnings = append(warnings, fmt.Sprintf("%s：filter 类型 %s 不在可视化注册表中，将保留原始 YAML", label, kind))
		}
		if len(r.Actions) == 0 {
			errors = append(errors, fmt.Sprintf("%s：至少需要一个 action", label))
		}
		for _, a := range r.Actions {
			ak, _ := a["typeKind"].(string)
			if ak == "" {
				errors = append(errors, fmt.Sprintf("%s：存在缺少 typeKind 的 action", label))
				continue
			}
			if _, ok := schema.FindAction(ak); !ok {
				warnings = append(warnings, fmt.Sprintf("%s：action 类型 %s 不在可视化注册表中，将保留原始 YAML", label, ak))
			}
		}
	}
	return errors, warnings
}

func validateRequired(kind string, m map[string]any, isFilter bool) error {
	schema := BuildSchema()
	var def TypeDef
	var ok bool
	if isFilter {
		def, ok = schema.FindFilter(kind)
	} else {
		def, ok = schema.FindAction(kind)
	}
	if !ok {
		return nil
	}
	for _, f := range def.Fields {
		if !f.Required {
			continue
		}
		if v, exists := getPath(m, f.Path); !exists || isEmpty(v) {
			return fmt.Errorf("%s 的必填字段 %s（%s）为空", kind, f.Path, f.Label)
		}
	}
	return nil
}

func ensureTagIdentifier(action map[string]any) {
	tag, _ := action["tag"].(map[string]any)
	if tag == nil {
		tag = map[string]any{}
		action["tag"] = tag
	}
	if id, _ := tag["identifier"].(string); strings.TrimSpace(id) != "" {
		return
	}
	value, _ := tag["value"].(string)
	tag["identifier"] = deterministicUUID(value)
}

func deterministicUUID(seed string) string {
	sum := sha256.Sum256([]byte("fluxzy-console-tag:" + seed))
	b := sum[:16]
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func expandDotted(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		if m, ok := v.(map[string]any); ok {
			v = expandDotted(m)
		}
		if !strings.Contains(k, ".") {
			out[k] = v
			continue
		}
		parts := strings.Split(k, ".")
		cur := out
		for i, p := range parts {
			if i == len(parts)-1 {
				cur[p] = v
				break
			}
			next, ok := cur[p].(map[string]any)
			if !ok {
				next = map[string]any{}
				cur[p] = next
			}
			cur = next
		}
	}
	return out
}

func normalizeMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = normalizeValue(v)
	}
	return out
}

func normalizeValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		return normalizeMap(t)
	case map[any]any:
		m := make(map[string]any, len(t))
		for k, v := range t {
			m[fmt.Sprint(k)] = normalizeValue(v)
		}
		return m
	case []any:
		out := make([]any, 0, len(t))
		for _, x := range t {
			out = append(out, normalizeValue(x))
		}
		return out
	case float64:
		if t == float64(int64(t)) {
			return int64(t)
		}
		return t
	case json.Number:
		if i, err := t.Int64(); err == nil {
			return i
		}
		return t
	default:
		return v
	}
}

func removeEmpty(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, x := range t {
			if k == "typeKind" {
				out[k] = x
				continue
			}
			c := removeEmpty(x)
			if isEmpty(c) {
				continue
			}
			out[k] = c
		}
		return out
	case []any:
		out := make([]any, 0, len(t))
		for _, x := range t {
			c := removeEmpty(x)
			if isEmpty(c) {
				continue
			}
			out = append(out, c)
		}
		return out
	case string:
		return strings.TrimSpace(t)
	default:
		return v
	}
}

func isEmpty(v any) bool {
	if v == nil {
		return true
	}
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t) == ""
	case []any:
		return len(t) == 0
	case []string:
		return len(t) == 0
	case map[string]any:
		return len(t) == 0
	default:
		return false
	}
}

func valueNode(v any) *yaml.Node {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			if k != "typeKind" {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		if _, ok := t["typeKind"]; ok {
			keys = append([]string{"typeKind"}, keys...)
		}
		n := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		for _, k := range keys {
			n.Content = append(n.Content,
				&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: k},
				valueNode(t[k]))
		}
		return n
	case []any:
		n := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		for _, x := range t {
			n.Content = append(n.Content, valueNode(x))
		}
		return n
	case []map[string]any:
		n := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		for _, x := range t {
			n.Content = append(n.Content, valueNode(x))
		}
		return n
	case string:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: t}
	case int:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.Itoa(t)}
	case int64:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.FormatInt(t, 10)}
	case float64:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!float", Value: strconv.FormatFloat(t, 'f', -1, 64)}
	case bool:
		if t {
			return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "true"}
		}
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "false"}
	case nil:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}
	default:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: fmt.Sprint(t)}
	}
}

func asMap(v any) (map[string]any, bool) {
	switch t := v.(type) {
	case map[string]any:
		return t, true
	case map[any]any:
		m := make(map[string]any, len(t))
		for k, v := range t {
			m[fmt.Sprint(k)] = v
		}
		return m, true
	default:
		return nil, false
	}
}

func asSlice(v any) ([]any, bool) {
	switch t := v.(type) {
	case []any:
		return t, true
	case []map[string]any:
		out := make([]any, len(t))
		for i := range t {
			out[i] = t[i]
		}
		return out, true
	default:
		return nil, false
	}
}

func getPath(m map[string]any, path string) (any, bool) {
	parts := strings.Split(path, ".")
	var cur any = m
	for _, p := range parts {
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = mm[p]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

func ParseIntList(v any) ([]int, error) {
	items, ok := asSlice(v)
	if !ok {
		if s, ok := v.(string); ok {
			for _, p := range strings.Split(s, ",") {
				if strings.TrimSpace(p) == "" {
					continue
				}
				n, err := strconv.Atoi(strings.TrimSpace(p))
				if err != nil {
					return nil, err
				}
				items = append(items, n)
			}
		} else {
			return nil, fmt.Errorf("不是列表")
		}
	}
	out := make([]int, 0, len(items))
	for _, x := range items {
		switch t := x.(type) {
		case int:
			out = append(out, t)
		case int64:
			out = append(out, int(t))
		case float64:
			out = append(out, int(t))
		case string:
			n, err := strconv.Atoi(strings.TrimSpace(t))
			if err != nil {
				return nil, err
			}
			out = append(out, n)
		}
	}
	return out, nil
}
