package types

import (
	"fmt"
	"strings"

	"github.com/orkspace/orkestra/pkg/runtime/sentinel"
)

// UserDefinedNote is a named template expression declared in a Katalog or Motif.
// Once registered, the note is callable by name in any template expression
// across the Katalog — status fields, when: conditions, normalize, e2e assertions.
type UserDefinedNote struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description,omitempty"`
	// Expression is a Go template string evaluated by the Resolver.
	// Built-in notes and other user-defined notes (same Katalog or imported) are
	// available inside the expression. The result is always a string.
	Expression string `yaml:"expression"`
	// Shadow: true acknowledges that this note intentionally overrides a built-in.
	Shadow bool `yaml:"shadow,omitempty"`
}

// NoteRegistry holds user-defined notes merged from a Katalog and its spec.imports Motifs.
type NoteRegistry struct {
	Include   string            `yaml:"include,omitempty"`
	Functions []UserDefinedNote `yaml:"functions,omitempty"`
}

// Merge merges src into nr. Local (nr) entries override src entries with the same name.
// Two src entries (from different Motifs) with the same name are a hard error.
func (nr NoteRegistry) Merge(src NoteRegistry, srcLabel string) (NoteRegistry, error) {
	if len(src.Functions) == 0 {
		return nr, nil
	}
	existing := make(map[string]bool, len(nr.Functions))
	for _, n := range nr.Functions {
		existing[n.Name] = true
	}
	result := NoteRegistry{Functions: make([]UserDefinedNote, len(nr.Functions))}
	copy(result.Functions, nr.Functions)
	for _, n := range src.Functions {
		if existing[n.Name] {
			// Local note with same name — local wins silently (intentional override).
			continue
		}
		existing[n.Name] = true
		result.Functions = append(result.Functions, n)
	}
	return result, nil
}

// MergeImport merges src into nr detecting conflicts between two non-local sources.
// Used when accumulating notes from multiple spec.imports Motifs.
func (nr NoteRegistry) MergeImport(src NoteRegistry, srcLabel string, seen map[string]string) (NoteRegistry, error) {
	result := NoteRegistry{Functions: make([]UserDefinedNote, len(nr.Functions))}
	copy(result.Functions, nr.Functions)
	for _, n := range src.Functions {
		if prev, conflict := seen[n.Name]; conflict {
			return NoteRegistry{}, fmt.Errorf(
				"note conflict: %q declared in both %s and %s — rename one or declare a local override in the Katalog's notes: block",
				n.Name, prev, srcLabel,
			)
		}
		seen[n.Name] = srcLabel
		result.Functions = append(result.Functions, n)
	}
	return result, nil
}

// IsNoteRef reports whether field is a user-defined note call — {{ noteName }}
// where noteName does not start with a dot (data ref) and is not a sentinel.
func IsNoteRef(field string) bool {
	if !strings.HasPrefix(field, "{{") {
		return false
	}
	inner := strings.TrimSpace(strings.TrimPrefix(field, "{{"))
	if strings.HasPrefix(inner, ".") {
		return false
	}
	name := strings.TrimSpace(strings.TrimSuffix(inner, "}}"))
	return name != "" && !sentinel.IsValid(name)
}

// NoteRefName extracts the bare note name from a {{ noteName }} field expression.
// Assumes IsNoteRef returned true for the same field.
func NoteRefName(field string) string {
	inner := strings.TrimSpace(strings.TrimPrefix(field, "{{"))
	return strings.TrimSpace(strings.TrimSuffix(inner, "}}"))
}

// ExpressionFor returns the template expression body for the named note,
// or an empty string if no note with that name exists in the registry.
func (nr NoteRegistry) ExpressionFor(name string) string {
	for _, n := range nr.Functions {
		if n.Name == name {
			return n.Expression
		}
	}
	return ""
}

// ContainsInExpression reports whether the named note's expression body, or any
// note it transitively calls, contains substr. Cycle-safe via a visited set.
func (nr NoteRegistry) ContainsInExpression(name, substr string) bool {
	return nr.containsInExpression(name, substr, make(map[string]bool))
}

func (nr NoteRegistry) containsInExpression(name, substr string, visited map[string]bool) bool {
	if visited[name] {
		return false
	}
	visited[name] = true
	expr := nr.ExpressionFor(name)
	if expr == "" {
		return false
	}
	if strings.Contains(expr, substr) {
		return true
	}
	for _, n := range nr.Functions {
		if !visited[n.Name] && isWordIn(expr, n.Name) {
			if nr.containsInExpression(n.Name, substr, visited) {
				return true
			}
		}
	}
	return false
}

// isWordIn reports whether word appears in s as a whole identifier token —
// not as a substring of a longer identifier on either side.
func isWordIn(s, word string) bool {
	if word == "" {
		return false
	}
	i := strings.Index(s, word)
	for i >= 0 {
		end := i + len(word)
		leftOk := i == 0 || !isIdentByte(s[i-1])
		rightOk := end == len(s) || !isIdentByte(s[end])
		if leftOk && rightOk {
			return true
		}
		next := strings.Index(s[i+1:], word)
		if next < 0 {
			break
		}
		i = i + 1 + next
	}
	return false
}

// isIdentByte reports whether b is a valid Go identifier continuation byte.
func isIdentByte(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

// Empty reports whether the registry contains no notes.
func (nr NoteRegistry) Empty() bool {
	return len(nr.Functions) == 0
}
