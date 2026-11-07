package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

//go:embed index.html
var indexHTML []byte

//go:embed icon.svg
var iconSVG []byte

//go:embed favicon.png
var faviconPNG []byte

//go:embed apple-touch-icon.png
var appleTouchIconPNG []byte

const systemPrompt = `You help children aged 5-7 learn to read English.
The child types one word or a few words, possibly misspelled. Reply with JSON only, no other text:
{"words": ["<typed word 1, spelled correctly, lowercase>", "<typed word 2>", ...], "sentence": "<the sentence>"}
"words" has one entry per typed word, in the order typed, each one a single word.

Rules for the sentence:
- Very simple, common words a 5-year-old can sound out.
  One typed word: 4 to 7 words. Several typed words: as short as possible, at most 12 words.
- Present tense, cheerful, concrete. Starts with a capital letter, ends with a period.
- Never drop a typed word, even small or odd ones like "hello", "yes", "the" or "run".
  The sentence contains every word from "words".
- If the typed words go together as a phrase ("tiny robot", "red ball"), keep them together
  in that order: "The boy builds a tiny robot."
- If they don't go together ("cat dog"), use each word on its own, in any place, even in two
  short parts: "The boy has a cat, and the girl has a dog."
Names of people, pets, places and countries are welcome: keep them (fix the spelling if needed).
If a word is not suitable for young children, or is not a real word, pick a friendly,
similar-sounding everyday word instead and use that in "words". Never repeat rude words.`

// One to five words of letters, single spaces between them.
var validWord = regexp.MustCompile(`^[a-z]+( [a-z]+){0,4}$`)

type sentenceResult struct {
	Original string `json:"original"`
	Word     string `json:"word"`
	Sentence string `json:"sentence"`
	// Fixes lists each misspelled word with its correction, one per typed word that changed.
	Fixes []fix `json:"fixes"`
}

type fix struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type server struct {
	apiKey string
	model  string
	client *http.Client
}

func main() {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		log.Fatal("ANTHROPIC_API_KEY is not set")
	}
	model := os.Getenv("ANTHROPIC_MODEL")
	if model == "" {
		model = "claude-haiku-5-5"
	}
	s := &server{apiKey: apiKey, model: model, client: &http.Client{Timeout: 10 * time.Second}}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		// Make the projector's browser pick up new versions after a deploy.
		w.Header().Set("Cache-Control", "no-cache")
		w.Write(indexHTML)
	})
	serve := func(contentType string, data []byte) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", contentType)
			w.Write(data)
		}
	}
	mux.HandleFunc("GET /icon.svg", serve("image/svg+xml", iconSVG))
	mux.HandleFunc("GET /favicon.png", serve("image/png", faviconPNG))
	// Browsers ask for /favicon.ico on their own; a PNG there is accepted.
	mux.HandleFunc("GET /favicon.ico", serve("image/png", faviconPNG))
	mux.HandleFunc("GET /apple-touch-icon.png", serve("image/png", appleTouchIconPNG))
	mux.HandleFunc("POST /api/sentence", s.handleSentence)

	log.Printf("listening on :8080 (model %s)", model)
	log.Fatal(http.ListenAndServe(":8080", mux))
}

func (s *server) handleSentence(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Word string `json:"word"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1024)).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	word := strings.ToLower(strings.Join(strings.Fields(req.Word), " "))
	if len(word) > 60 || !validWord.MatchString(word) {
		http.Error(w, "up to 5 words, letters only", http.StatusBadRequest)
		return
	}

	// Haiku sometimes drops a typed word; ask once more before settling.
	var res *sentenceResult
	var err error
	for attempt := 0; attempt < 2; attempt++ {
		res, err = s.makeSentence(r.Context(), word)
		if err != nil || keepsAllWords(word, res) {
			break
		}
		log.Printf("sentence for %q dropped a word: %+v", word, res)
	}
	if err != nil {
		log.Printf("sentence for %q: %v", word, err)
		http.Error(w, "try again", http.StatusBadGateway)
		return
	}
	res.Fixes = fixes(word, res.Word)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(res)
}

func (s *server) makeSentence(ctx context.Context, word string) (*sentenceResult, error) {
	body, _ := json.Marshal(map[string]any{
		"model":      s.model,
		"max_tokens": 200,
		// Haiku 5.5 thinks by default; on some words that used up max_tokens and left no text.
		"thinking": map[string]string{"type": "disabled"},
		"system":   systemPrompt,
		"messages": []map[string]string{{"role": "user", "content": word}},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.anthropic.com/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", s.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("content-type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("anthropic %d: %s", resp.StatusCode, raw)
	}

	var msg struct {
		StopReason string `json:"stop_reason"`
		Content    []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &msg); err != nil {
		return nil, err
	}
	var text string
	for _, c := range msg.Content {
		if c.Type == "text" {
			text += c.Text
		}
	}
	// Tolerate stray text or code fences around the JSON object.
	start, end := strings.Index(text, "{"), strings.LastIndex(text, "}")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("no JSON in reply (stop_reason %s): %q", msg.StopReason, text)
	}
	var reply struct {
		Words    []string `json:"words"`
		Sentence string   `json:"sentence"`
	}
	if err := json.Unmarshal([]byte(text[start:end+1]), &reply); err != nil {
		return nil, fmt.Errorf("bad JSON in reply: %q", text)
	}
	var out sentenceResult
	out.Word = strings.ToLower(strings.Join(strings.Fields(strings.Join(reply.Words, " ")), " "))
	out.Sentence = strings.TrimSpace(reply.Sentence)
	if out.Word == "" || out.Sentence == "" {
		return nil, errors.New("empty word or sentence")
	}
	out.Original = word
	return &out, nil
}

// keepsAllWords reports whether the reply kept one corrected word per typed word
// and the sentence contains each of them.
func keepsAllWords(typed string, res *sentenceResult) bool {
	words := strings.Fields(res.Word)
	if len(words) != len(strings.Fields(typed)) {
		return false
	}
	sentence := strings.ToLower(res.Sentence)
	for _, w := range words {
		if !regexp.MustCompile(`\b` + regexp.QuoteMeta(w)).MatchString(sentence) {
			return false
		}
	}
	return true
}

// fixes pairs each typed word with its corrected form and keeps the ones that changed.
// If the word counts differ, the pairing is unknown, so the whole phrase is one fix.
func fixes(typed, corrected string) []fix {
	from, to := strings.Fields(typed), strings.Fields(corrected)
	if len(from) != len(to) {
		if typed == corrected {
			return []fix{}
		}
		return []fix{{From: typed, To: corrected}}
	}
	out := []fix{}
	for i := range from {
		if from[i] != to[i] {
			out = append(out, fix{From: from[i], To: to[i]})
		}
	}
	return out
}
