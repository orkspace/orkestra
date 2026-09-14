# Adding a New Sentinel

Sentinels are computed in `sentinel.go` by comparing `oldObj` and `newObj` at `UpdateFunc` time. Adding a new one is four steps in the same file, plus a test and a doc entry.

---

## 1. Declare the constant

```go
const (
    GenerationChanged  Sentinel = "generationChanged"
    LabelsChanged      Sentinel = "labelsChanged"
    // ...
    MyNewSentinel      Sentinel = "myNewSentinel"  // ← add here
)
```

The constant name is PascalCase. The string value is camelCase — this is what users write in YAML.

---

## 2. Register it in `ValidSentinels`

```go
func ValidSentinels() []string {
    return []string{
        string(GenerationChanged),
        // ...
        string(MyNewSentinel),  // ← add here, in declaration order
    }
}
```

`ValidSentinels()` is used by validators to produce error messages with the full list. `IsValid` and `IsAllValid` relies on it for runtime and test check respectively.

---

## 3. Implement the comparison in `computeOne`

```go
func computeOne(name string, old, new metav1.Object) string {
    switch Sentinel(name) {
    // ...existing cases...
    case MyNewSentinel:
        return boolStr( /* compare old and new */ )
    default:
        return ""
    }
}
```

The comparison must use only fields available on `metav1.Object` (the interface). If you need fields from a concrete type (`*appsv1.Deployment`, for example), you cannot add this sentinel here — keep this package stdlib+apimachinery only. Move the computation into `pkg/runtime/informer` instead and keep the name constant here.

---

## 4. Write the tests

Add test cases to `sentinel_test.go` following the existing pattern. Cover:

- The sentinel is `"true"` when the condition holds
- The sentinel is `"false"` when the condition does not hold
- The sentinel is not computed when not declared (use `TestCompute_OnlyDeclaredAreComputed` as reference)

```go
func TestCompute_MyNewSentinel_True(t *testing.T) {
    old := &metav1.ObjectMeta{/* state before */}
    new := &metav1.ObjectMeta{/* state after */}
    result := Compute([]string{string(MyNewSentinel)}, old, new)
    assert.Equal(t, "true", result[string(MyNewSentinel)])
}

func TestCompute_MyNewSentinel_False(t *testing.T) {
    old := &metav1.ObjectMeta{/* same state */}
    new := &metav1.ObjectMeta{/* same state */}
    result := Compute([]string{string(MyNewSentinel)}, old, new)
    assert.Equal(t, "false", result[string(MyNewSentinel)])
}
```

---

## 5. Document it

Add an entry to [01-sentinel-names.md](01-sentinel-names.md) following the existing format:

- The comparison expression
- What it tests
- When to use it
- Any edge cases or gotchas (e.g. resources that do not increment `generation`)

The sentinel will not be in the schema reference or the user-facing validator error messages until it appears in `ValidSentinels()` — that is already handled by step 2.

→ [README](../README.md) — package overview, YAML usage, and the sentinel vs Note distinction
