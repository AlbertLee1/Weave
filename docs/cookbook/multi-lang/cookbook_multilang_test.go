// Verifies the multi-language SDK cookbook (US-424) ships every chapter
// the PRD names, with all four language tabs present (Python /
// TypeScript / Go / Java) and a README index that lists every chapter.
//
// Mirrors docs/cookbook/cookbook_test.go in spirit but ALL gates are
// structural, no toolchain probing — markdown content is portable across
// CI runners.
package multilang_test

import (
	"os"
	"strings"
	"testing"
)

// chapters is the canonical 10-chapter list named by the PRD AC for
// US-424: "10 章节覆盖：login / load / aggregate / action / subscribe /
// saga / function / branch / lineage / batch". Update this slice in
// lockstep with new files; the README index must list every entry.
var chapters = []string{
	"01-login",
	"02-load",
	"03-aggregate",
	"04-action",
	"05-subscribe",
	"06-saga",
	"07-function",
	"08-branch",
	"09-lineage",
	"10-batch",
}

// languageTabs is the canonical fenced-code-block language identifier
// every chapter must include at least once. Markdown renderers map the
// identifier to a syntax-highlighter; the AC requires py / ts / go /
// java tabs in every chapter.
var languageTabs = []string{
	"```python",
	"```typescript",
	"```go",
	"```java",
}

// TestCookbook_StructureMatches asserts every chapter ships a markdown
// file under the same stem and a README at the directory root.
func TestCookbook_StructureMatches(t *testing.T) {
	must := []string{"README.md"}
	for _, c := range chapters {
		must = append(must, c+".md")
	}
	for _, name := range must {
		if _, err := os.Stat(name); err != nil {
			t.Errorf("missing %s: %v", name, err)
		}
	}
}

// TestCookbook_AllChaptersHaveFourLanguageTabs walks every chapter and
// asserts the four language code-fence markers appear at least once.
// Catches a doc edit that drops a tab without updating the index.
func TestCookbook_AllChaptersHaveFourLanguageTabs(t *testing.T) {
	for _, c := range chapters {
		path := c + ".md"
		t.Run(path, func(t *testing.T) {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			body := string(raw)
			for _, tab := range languageTabs {
				if !strings.Contains(body, tab) {
					t.Errorf("%s missing %s code block", path, tab)
				}
			}
		})
	}
}

// TestCookbook_ReadmeListsEveryChapter ensures the index table at the
// directory root references every chapter file. A new chapter file
// without a README entry is invisible to readers, so CI catches it.
func TestCookbook_ReadmeListsEveryChapter(t *testing.T) {
	raw, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatalf("read README.md: %v", err)
	}
	body := string(raw)
	for _, c := range chapters {
		marker := c + ".md"
		if !strings.Contains(body, marker) {
			t.Errorf("README.md does not reference %s", marker)
		}
	}
}
