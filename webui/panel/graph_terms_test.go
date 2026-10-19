// SPDX-License-Identifier: AGPL-3.0-or-later

package panel

import (
	"reflect"
	"testing"
)

func termNames(hits []termHit) []string {
	out := make([]string, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.Term)
	}
	return out
}

// The closure rule is what keeps the knowledge layer readable: a fragment
// whose every occurrence sits inside a longer repeated phrase must not become
// a node of its own ("护链" inside "巡护链路").
func TestExtractTermsClosedPhrases(t *testing.T) {
	docs := []graphText{
		{id: "daily:2026-01-02", content: "巡护链路 只读排查\n巡护链路 只读排查\n巡护链路 只读排查\n"},
	}
	got := termNames(extractTerms(docs, nil, 0))
	want := []string{"只读排查", "巡护链路"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("terms = %v, want %v", got, want)
	}
}

// Latin words need two occurrences; quoted names are explicit and survive on
// one, but an over-long quote is not a node.
func TestExtractTermsLatinAndQuoted(t *testing.T) {
	docs := []graphText{
		{id: "core:memory", content: "dog dog telemetry 「巡检机器人」 「提取遥测统计数字（紧凑表格，只读）」"},
	}
	got := termNames(extractTerms(docs, nil, 0))
	want := []string{"dog", "巡检机器人"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("terms = %v, want %v", got, want)
	}
}

// Terms already carrying a node (surface labels, topic/project/skill names)
// and generic phrases never duplicate into the knowledge layer.
func TestExtractTermsExclusions(t *testing.T) {
	docs := []graphText{
		{id: "daily:2026-01-02", content: "任务 任务 任务 work work work dog dog"},
	}
	exclude := map[string]bool{"work": true}
	got := termNames(extractTerms(docs, exclude, 0))
	want := []string{"dog"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("terms = %v, want %v", got, want)
	}
}

// Function-character-only bigrams carry no phrase signal and are skipped even
// when they repeat.
func TestExtractTermsSkipsFunctionBigrams(t *testing.T) {
	docs := []graphText{
		{id: "core:memory", content: "的了 的了 的了"},
	}
	if got := termNames(extractTerms(docs, nil, 0)); len(got) != 0 {
		t.Fatalf("terms = %v, want none", got)
	}
}

// The output is deterministic — count desc, term asc — and the cap keeps the
// strongest terms.
func TestExtractTermsDeterministicAndCapped(t *testing.T) {
	docs := []graphText{
		{id: "core:memory", content: "aaa aaa aaa bbb bbb bbb ccc ccc ccc\n"},
	}
	first := extractTerms(docs, nil, 2)
	second := extractTerms(docs, nil, 2)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("extraction not deterministic: %v vs %v", termNames(first), termNames(second))
	}
	if got := termNames(first); !reflect.DeepEqual(got, []string{"aaa", "bbb"}) {
		t.Fatalf("capped terms = %v, want [aaa bbb]", got)
	}
}

// Evidence samples are the literal source lines the term appears in —
// bounded, deduplicated, oldest-dated first ("daily:YYYY-MM-DD" sorts
// chronologically).
func TestCollectEvidence(t *testing.T) {
	docs := []graphText{
		{id: "daily:2026-01-03", content: "巡护链路 复检\n"},
		{id: "daily:2026-01-02", content: "巡护链路 只读排查\n巡护链路 只读排查\n"},
	}
	terms := extractTerms(docs, nil, 0)
	collectEvidence(terms, docs)
	var hit *termHit
	for i := range terms {
		if terms[i].Term == "巡护链路" {
			hit = &terms[i]
		}
	}
	if hit == nil {
		t.Fatal("巡护链路 missing from extracted terms")
	}
	want := []string{"巡护链路 只读排查", "巡护链路 复检"}
	if !reflect.DeepEqual(hit.Samples, want) {
		t.Fatalf("samples = %v, want %v", hit.Samples, want)
	}
}

func TestHanRunsAndLatinWords(t *testing.T) {
	runs := hanRuns("部署 v2 完成，巡检 ok")
	if want := []string{"部署", "完成", "巡检"}; !reflect.DeepEqual(runs, want) {
		t.Fatalf("hanRuns = %v, want %v", runs, want)
	}
	words := latinWords("Work-Report utf8 3d dog_2")
	if want := []string{"work-report", "utf8", "dog_2"}; !reflect.DeepEqual(words, want) {
		t.Fatalf("latinWords = %v, want %v", words, want)
	}
}
