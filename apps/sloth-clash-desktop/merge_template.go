package main

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// applyProfileMergeTemplate applies a Clash Verge–style enhancement YAML on top of doc.
// Supported:
//   - Top-level scalar/map keys (except prepend/append/delete) deep-merge into doc.
//   - prepend / append: rules, proxy-groups ([]), proxy-providers, rule-providers (maps).
//   - delete: rules ([]string exact lines), proxy-groups ([] names), proxy-providers, rule-providers ([] names).
func applyProfileMergeTemplate(doc map[string]any, template string) error {
	template = strings.TrimSpace(template)
	if template == "" {
		return nil
	}
	var tpl map[string]any
	if err := yaml.Unmarshal([]byte(template), &tpl); err != nil {
		return fmt.Errorf("merge template: %w", err)
	}
	if len(tpl) == 0 {
		return nil
	}

	reserved := map[string]bool{
		"prepend": true,
		"append":  true,
		"delete":  true,
	}
	for k, v := range tpl {
		if reserved[k] {
			continue
		}
		if srcMap, ok := v.(map[string]any); ok {
			if dstMap, ok := doc[k].(map[string]any); ok {
				deepMergeMap(dstMap, srcMap)
				doc[k] = dstMap
				continue
			}
		}
		doc[k] = v
	}

	if prep, ok := tpl["prepend"].(map[string]any); ok {
		mergeListPrepend(doc, "rules", prep["rules"])
		mergeListPrepend(doc, "proxy-groups", prep["proxy-groups"])
		mergeMapMerge(doc, "proxy-providers", prep["proxy-providers"], false)
		mergeMapMerge(doc, "rule-providers", prep["rule-providers"], false)
	}
	if app, ok := tpl["append"].(map[string]any); ok {
		mergeListAppend(doc, "rules", app["rules"])
		mergeListAppend(doc, "proxy-groups", app["proxy-groups"])
		mergeMapMerge(doc, "proxy-providers", app["proxy-providers"], true)
		mergeMapMerge(doc, "rule-providers", app["rule-providers"], true)
	}
	if del, ok := tpl["delete"].(map[string]any); ok {
		deleteRulesExact(doc, del["rules"])
		deleteProxyGroupsByName(doc, del["proxy-groups"])
		deleteMapKeys(doc, "proxy-providers", del["proxy-providers"])
		deleteMapKeys(doc, "rule-providers", del["rule-providers"])
	}
	return nil
}

func deepMergeMap(dst, src map[string]any) {
	for k, v := range src {
		if srcChild, ok := v.(map[string]any); ok {
			if dstChild, ok := dst[k].(map[string]any); ok {
				deepMergeMap(dstChild, srcChild)
				dst[k] = dstChild
				continue
			}
		}
		dst[k] = v
	}
}

func mergeListPrepend(doc map[string]any, key string, patch any) {
	if patch == nil {
		return
	}
	add, ok := patch.([]any)
	if !ok || len(add) == 0 {
		return
	}
	base := listGet(doc, key)
	doc[key] = append(add, base...)
}

func mergeListAppend(doc map[string]any, key string, patch any) {
	if patch == nil {
		return
	}
	add, ok := patch.([]any)
	if !ok || len(add) == 0 {
		return
	}
	base := listGet(doc, key)
	doc[key] = append(base, add...)
}

func listGet(doc map[string]any, key string) []any {
	raw, ok := doc[key]
	if !ok || raw == nil {
		return nil
	}
	arr, ok := raw.([]any)
	if !ok {
		return nil
	}
	return arr
}

func mergeMapMerge(doc map[string]any, key string, patch any, appendOrder bool) {
	if patch == nil {
		return
	}
	pm, ok := patch.(map[string]any)
	if !ok || len(pm) == 0 {
		return
	}
	dst := mapGet(doc, key)
	if appendOrder {
		for k, v := range pm {
			dst[k] = v
		}
		doc[key] = dst
		return
	}
	// prepend merge: incoming keys win over existing (Verge-style overrides).
	merged := map[string]any{}
	for k, v := range pm {
		merged[k] = v
	}
	for k, v := range dst {
		if _, exists := merged[k]; !exists {
			merged[k] = v
		}
	}
	doc[key] = merged
}

func mapGet(doc map[string]any, key string) map[string]any {
	raw, ok := doc[key]
	if !ok || raw == nil {
		return map[string]any{}
	}
	if m, ok := raw.(map[string]any); ok {
		out := make(map[string]any, len(m))
		for k, v := range m {
			out[k] = v
		}
		return out
	}
	return map[string]any{}
}

func deleteRulesExact(doc map[string]any, patch any) {
	remove, ok := stringListFromAny(patch)
	if !ok || len(remove) == 0 {
		return
	}
	rm := map[string]bool{}
	for _, s := range remove {
		rm[s] = true
	}
	arr := listGet(doc, "rules")
	if len(arr) == 0 {
		return
	}
	out := make([]any, 0, len(arr))
	for _, it := range arr {
		s, ok := it.(string)
		if ok && rm[s] {
			continue
		}
		out = append(out, it)
	}
	doc["rules"] = out
}

func deleteProxyGroupsByName(doc map[string]any, patch any) {
	names, ok := stringListFromAny(patch)
	if !ok || len(names) == 0 {
		return
	}
	rm := map[string]bool{}
	for _, n := range names {
		rm[strings.TrimSpace(n)] = true
	}
	arr := listGet(doc, "proxy-groups")
	if len(arr) == 0 {
		return
	}
	out := make([]any, 0, len(arr))
	for _, it := range arr {
		gm, ok := it.(map[string]any)
		if !ok {
			out = append(out, it)
			continue
		}
		n, _ := gm["name"].(string)
		if rm[strings.TrimSpace(n)] {
			continue
		}
		out = append(out, it)
	}
	doc["proxy-groups"] = out
}

func deleteMapKeys(doc map[string]any, key string, patch any) {
	keys, ok := stringListFromAny(patch)
	if !ok || len(keys) == 0 {
		return
	}
	m := mapGet(doc, key)
	for _, k := range keys {
		delete(m, strings.TrimSpace(k))
	}
	doc[key] = m
}

func stringListFromAny(patch any) ([]string, bool) {
	arr, ok := patch.([]any)
	if !ok {
		return nil, false
	}
	out := make([]string, 0, len(arr))
	for _, it := range arr {
		s, ok := it.(string)
		if !ok {
			continue
		}
		out = append(out, s)
	}
	return out, true
}
