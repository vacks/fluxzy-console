package rules

import "testing"

func TestRenderAndParseNestedFilterCollection(t *testing.T) {
	model := RuleModel{Rules: []RuleDraft{{
		Name:    "多条件请求体改写",
		Enabled: true,
		Filter: map[string]any{
			"typeKind":  "FilterCollection",
			"operation": "And",
			"children": []any{
				map[string]any{
					"typeKind":      "HostFilter",
					"pattern":       "example.com",
					"operation":     "Exact",
					"caseSensitive": false,
					"inverted":      false,
				},
				map[string]any{
					"typeKind":  "FilterCollection",
					"operation": "Or",
					"children": []any{
						map[string]any{
							"typeKind":      "PathFilter",
							"pattern":       "/api/login",
							"operation":     "StartsWith",
							"caseSensitive": true,
							"inverted":      false,
						},
						map[string]any{
							"typeKind":      "PathFilter",
							"pattern":       "/api/token",
							"operation":     "StartsWith",
							"caseSensitive": true,
							"inverted":      false,
						},
					},
				},
			},
		},
		Actions: []map[string]any{{
			"typeKind":     "ConsoleTransformRequestBodyAction",
			"operation":    "Replace",
			"pattern":      "hello",
			"replacement":  "patched",
			"maxBodyBytes": int64(8388608),
		}},
	}}}

	rendered, err := Render(model)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	parsed, err := Parse([]byte(rendered))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(parsed.Rules) != 1 {
		t.Fatalf("rules = %d, want 1", len(parsed.Rules))
	}
	filter := parsed.Rules[0].Filter
	if filter["typeKind"] != "FilterCollection" || filter["operation"] != "And" {
		t.Fatalf("unexpected root filter: %#v", filter)
	}
	children, ok := filter["children"].([]any)
	if !ok || len(children) != 2 {
		t.Fatalf("root children = %#v, want 2", filter["children"])
	}
	first, _ := children[0].(map[string]any)
	if first["typeKind"] != "HostFilter" || first["pattern"] != "example.com" {
		t.Fatalf("unexpected host child: %#v", first)
	}
	group, _ := children[1].(map[string]any)
	if group["typeKind"] != "FilterCollection" || group["operation"] != "Or" {
		t.Fatalf("unexpected nested group: %#v", group)
	}
	nested, ok := group["children"].([]any)
	if !ok || len(nested) != 2 {
		t.Fatalf("nested children = %#v, want 2", group["children"])
	}

	errors, _ := ValidateStructure([]byte(rendered))
	if len(errors) != 0 {
		t.Fatalf("ValidateStructure errors: %v", errors)
	}
}
