// File: pkg/compiler/typechecker_fields.go
package compiler

// fieldMatch describes a field found by findFieldMatches, including where in
// the (possibly nested) embedding chain it was located.
type fieldMatch struct {
	Owner string    // name of the struct that directly declares Field
	Field FieldDecl // the field declaration itself
	Path  []string  // embedding path from the root struct to Owner
}

// findFieldMatches returns every struct field matching member, walking
// anonymous (embedded) structs recursively. The seen map guards against
// cycles in embedding graphs and is shared across the recursion.
func (tc *Typechecker) findFieldMatches(structName, member string, path []string, seen map[string]bool) []fieldMatch {
	if seen == nil {
		seen = map[string]bool{}
	}
	var matches []fieldMatch
	key := structName
	if seen[key] {
		return nil
	}
	seen[key] = true

	fields, ok := tc.Global.Structs[structName]
	if !ok {
		return nil
	}
	for _, f := range fields {
		if f.Name == member {
			matches = append(matches, fieldMatch{
				Owner: structName,
				Field: f,
				Path:  append([]string(nil), path...),
			})
		}
		if f.Anonymous {
			embedded := typeBaseName(f.Type)
			subPath := append(append([]string(nil), path...), embedded)
			matches = append(matches, tc.findFieldMatches(embedded, member, subPath, seen)...)
		}
	}
	return matches
}
