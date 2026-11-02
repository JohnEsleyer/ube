package compiler

// fieldMatch describes a field reached directly or through a chain of
// anonymously embedded structs. Path holds the embedded field names that must
// be traversed before reaching Field.
type fieldMatch struct {
	Field FieldDecl
	Owner string
	Path  []string
}

// methodMatch describes a method promoted from an anonymously embedded struct.
type methodMatch struct {
	Sig   FuncSignature
	Owner string
	Path  []string
}

func (tc *Typechecker) fieldsOf(typeName string) []FieldDecl {
	if meta, ok := tc.Global.StructMeta[typeName]; ok {
		return meta.Fields
	}
	return tc.Global.Structs[typeName]
}

func cloneSeen(seen map[string]bool) map[string]bool {
	out := make(map[string]bool, len(seen)+1)
	for k, v := range seen {
		out[k] = v
	}
	return out
}

// findFieldMatches finds all fields named member on typeName. A direct field
// shadows embedded fields. Two or more matches through different embedded
// structs are reported as an ambiguity by the caller.
func (tc *Typechecker) findFieldMatches(typeName, member string, path []string, seen map[string]bool) []fieldMatch {
	if seen[typeName] {
		return nil
	}
	fields := tc.fieldsOf(typeName)

	var direct []fieldMatch
	for _, f := range fields {
		if f.Name == member {
			direct = append(direct, fieldMatch{Field: f, Owner: typeName, Path: append([]string(nil), path...)})
		}
	}
	if len(direct) > 0 {
		return direct
	}

	nextSeen := cloneSeen(seen)
	nextSeen[typeName] = true

	var matches []fieldMatch
	for _, f := range fields {
		if !f.Anonymous {
			continue
		}
		embedded := typeBaseName(f.Type)
		if embedded == "" {
			continue
		}
		nextPath := append(append([]string(nil), path...), f.Name)
		matches = append(matches, tc.findFieldMatches(embedded, member, nextPath, nextSeen)...)
	}
	return matches
}

// findMethodMatches finds methods named method on typeName or promoted from an
// anonymously embedded struct. A direct method shadows embedded methods.
func (tc *Typechecker) findMethodMatches(typeName, method string, path []string, seen map[string]bool) []methodMatch {
	if seen[typeName] {
		return nil
	}
	if sig, ok := tc.Global.Funcs[typeName+"."+method]; ok {
		return []methodMatch{{Sig: sig, Owner: typeName, Path: append([]string(nil), path...)}}
	}

	fields := tc.fieldsOf(typeName)
	nextSeen := cloneSeen(seen)
	nextSeen[typeName] = true

	var matches []methodMatch
	for _, f := range fields {
		if !f.Anonymous {
			continue
		}
		embedded := typeBaseName(f.Type)
		if embedded == "" {
			continue
		}
		nextPath := append(append([]string(nil), path...), f.Name)
		matches = append(matches, tc.findMethodMatches(embedded, method, nextPath, nextSeen)...)
	}
	return matches
}
