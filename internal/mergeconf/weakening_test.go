package mergeconf

import (
	"reflect"
	"testing"
)

func TestComputeTestWeakening(t *testing.T) {
	cases := []struct {
		name     string
		changes  []FileChange
		want     TestWeakening
		weakened bool
	}{
		{"no changes", nil, TestWeakening{}, false},
		{
			"a deleted test file is listed, not counted again as removed assertions",
			[]FileChange{{Path: "pkg/a_test.go", Deleted: true, Removed: []string{`assert.Equal(t, 1, x)`}}},
			TestWeakening{DeletedTestFiles: []string{"pkg/a_test.go"}}, true,
		},
		{
			"skips across frameworks",
			[]FileChange{
				{Path: "a_test.go", Added: []string{`	t.Skip("flaky")`}},
				{Path: "tests/test_b.py", Added: []string{`@pytest.mark.skip(reason="later")`, `def test_b():`}},
				{Path: "src/c.test.ts", Added: []string{`it.skip("renders", () => {`, `xit("old", () => {})`}},
				{Path: "src/test/java/DTest.java", Added: []string{`    @Disabled`}},
				{Path: "tests/e.rs", Added: []string{`#[ignore]`}},
			},
			TestWeakening{SkipsAdded: 6}, true,
		},
		{
			"assertions removed outnumber assertions added",
			[]FileChange{{Path: "svc_test.go",
				Removed: []string{`	require.Equal(t, want, got)`, `	if got != want { t.Errorf("x") }`, `	assert.NoError(t, err)`},
				Added:   []string{`	assert.True(t, got != nil)`}}},
			TestWeakening{AssertionsRemoved: 3, AssertionsAdded: 1}, true,
		},
		{
			"a renamed assertion nets out",
			[]FileChange{{Path: "svc_test.go",
				Removed: []string{`	assert.Equal(t, want, got)`},
				Added:   []string{`	require.Equal(t, want, got)`}}},
			TestWeakening{AssertionsRemoved: 1, AssertionsAdded: 1}, false,
		},
		{
			"source files are not inspected for tests",
			[]FileChange{{Path: "internal/svc.go", Removed: []string{`assert(x > 0)`}, Added: []string{`t.Skip()`}}},
			TestWeakening{}, false,
		},
		{
			"documentation under a test directory is not a test",
			[]FileChange{{Path: "tests/README.md", Deleted: true}},
			TestWeakening{}, false,
		},
		{
			"a skip in a test file the change creates is not weakening",
			[]FileChange{{Path: "pkg/new_test.go", Created: true,
				Added: []string{`	t.Skip("needs a database")`, `	assert.Equal(t, 1, x)`}}},
			TestWeakening{AssertionsAdded: 1}, false,
		},
		{
			"markers inside string literals are data, not code",
			[]FileChange{{Path: "detector_test.go",
				Added:   []string{"	{Added: []string{`t.Skip(\"flaky\")`}},", `	want := "@pytest.mark.skip"`, `	name := 'it.skip('`},
				Removed: []string{`	msg := "assert.Equal(t, 1, x) failed"`}}},
			TestWeakening{}, false,
		},
		{
			"jest expectations and RSpec matchers count",
			[]FileChange{
				{Path: "src/x.spec.ts", Removed: []string{`expect(sum(1, 2)).toBe(3)`}},
				{Path: "spec/y_spec.rb", Removed: []string{`expect(user).to eq(admin)`}, Added: []string{`skip "pending"`}},
			},
			TestWeakening{AssertionsRemoved: 2, SkipsAdded: 1}, true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ComputeTestWeakening(tc.changes)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("ComputeTestWeakening = %+v, want %+v", got, tc.want)
			}
			if got.Weakened() != tc.weakened {
				t.Errorf("Weakened() = %t, want %t", got.Weakened(), tc.weakened)
			}
		})
	}
}

func TestComputeTestWeakening_sortsDeletedFiles(t *testing.T) {
	got := ComputeTestWeakening([]FileChange{
		{Path: "z_test.go", Deleted: true},
		{Path: "a_test.go", Deleted: true},
	})
	if want := []string{"a_test.go", "z_test.go"}; !reflect.DeepEqual(got.DeletedTestFiles, want) {
		t.Errorf("DeletedTestFiles = %v, want %v", got.DeletedTestFiles, want)
	}
}

func TestCountSuppressionsAdded(t *testing.T) {
	changes := []FileChange{
		{Path: "a.go", Added: []string{`x := y //nolint:errcheck`, `// nolint`, `plain()`}},
		{Path: "b.py", Added: []string{`import foo  # type: ignore`, `x = 1  # noqa: E501`}, Removed: []string{`# noqa`}},
		{Path: "c.ts", Added: []string{`// @ts-ignore`, `/* eslint-disable no-console */`, `// @ts-expect-error`}},
		{Path: "D.java", Added: []string{`@SuppressWarnings("unchecked")`}},
		{Path: "e.rs", Added: []string{`#[allow(dead_code)]`}},
		{Path: "f.rb", Added: []string{`# rubocop:disable Metrics/AbcSize`}},
		{Path: "g_test.go", Added: []string{`	hint := "add // nolint to silence it"`, "	{Added: []string{`# type: ignore`}},"}},
	}
	if got, want := CountSuppressionsAdded(changes), 10; got != want {
		t.Errorf("CountSuppressionsAdded = %d, want %d", got, want)
	}
	if got := CountSuppressionsAdded(nil); got != 0 {
		t.Errorf("CountSuppressionsAdded(nil) = %d", got)
	}
}
