package main

import (
	"reflect"
	"testing"
)

func TestKeepsAllWords(t *testing.T) {
	cases := []struct {
		typed, word, sentence string
		want                  bool
	}{
		{"hello bear", "bear", "The big brown bear eats honey.", false},
		{"hello bear", "hello bear", "The girl says hello to the bear.", true},
		{"elefant", "elephant", "The elephant is big.", true},
		{"cat dog", "cat dog", "The boy has a cat.", false},
		{"cat", "cat", "Cats nap in the sun.", true},
	}
	for _, c := range cases {
		got := keepsAllWords(c.typed, &sentenceResult{Word: c.word, Sentence: c.sentence})
		if got != c.want {
			t.Errorf("keepsAllWords(%q, %q, %q) = %v, want %v", c.typed, c.word, c.sentence, got, c.want)
		}
	}
}

func TestFixes(t *testing.T) {
	cases := []struct {
		typed, corrected string
		want             []fix
	}{
		{"cat", "cat", []fix{}},
		{"elefant", "elephant", []fix{{"elefant", "elephant"}}},
		{"elefant eats banan", "elephant eats banana", []fix{{"elefant", "elephant"}, {"banan", "banana"}}},
		{"hello bear", "bear", []fix{{"hello bear", "bear"}}},
	}
	for _, c := range cases {
		got := fixes(c.typed, c.corrected)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("fixes(%q, %q) = %v, want %v", c.typed, c.corrected, got, c.want)
		}
	}
}
