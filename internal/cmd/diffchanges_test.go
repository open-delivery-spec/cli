package cmd

import (
	"reflect"
	"testing"

	"github.com/open-delivery-spec/cli/internal/mergeconf"
)

func TestParseFileChanges(t *testing.T) {
	diff := `diff --git a/pkg/a_test.go b/pkg/a_test.go
index 1111111..2222222 100644
--- a/pkg/a_test.go
+++ b/pkg/a_test.go
@@ -1,4 +1,4 @@
 package pkg
-	assert.Equal(t, 1, x)
+	t.Skip("later")
 // --- not a header inside a hunk
diff --git a/old_test.go b/old_test.go
deleted file mode 100644
index 3333333..0000000
--- a/old_test.go
+++ /dev/null
@@ -1,2 +0,0 @@
-package old
-func TestOld(t *testing.T) {}
diff --git a/new_test.go b/new_test.go
new file mode 100644
index 0000000..4444444
--- /dev/null
+++ b/new_test.go
@@ -0,0 +1 @@
+package fresh
diff --git a/README.md b/README.md
--- a/README.md
+++ b/README.md
@@ -1 +1 @@
-old
+new
diff --git a/moved.go b/renamed.go
similarity index 100%
rename from moved.go
rename to renamed.go
diff --git "a/dir with space/x.go" "b/dir with space/x.go"
--- "a/dir with space/x.go"	
+++ "b/dir with space/x.go"	
@@ -0,0 +1 @@
+package x
`
	got := parseFileChanges([]byte(diff))
	want := []mergeconf.FileChange{
		{Path: "pkg/a_test.go", Added: []string{`	t.Skip("later")`}, Removed: []string{`	assert.Equal(t, 1, x)`}},
		{Path: "old_test.go", Deleted: true, Removed: []string{"package old", "func TestOld(t *testing.T) {}"}},
		{Path: "new_test.go", Created: true, Added: []string{"package fresh"}},
		{Path: "dir with space/x.go", Added: []string{"package x"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseFileChanges =\n%+v\nwant\n%+v", got, want)
	}
}

func TestWithoutPaths(t *testing.T) {
	if got := withoutPaths([]string{"a", "b", "c"}, []string{"b"}); !reflect.DeepEqual(got, []string{"a", "c"}) {
		t.Errorf("withoutPaths = %v", got)
	}
	in := []string{"a"}
	if got := withoutPaths(in, nil); !reflect.DeepEqual(got, in) {
		t.Errorf("withoutPaths(nil drop) = %v", got)
	}
}
