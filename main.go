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

const systemPrompt = `You help children aged 5-7 learn to read English.
The child types one word, possibly misspelled. Reply with JSON only, no other text:
{"word": "<the word, spelled correctly, lowercase>", "sentence": "<one sentence>"}

Rules for the sentence:
- 4 to 7 very simple, common words a 5-year-old can sound out.
- Present tense, cheerful, concrete. Starts with a capital letter, ends with a period.
- It must contain the corrected word exactly as written in "word".
Names of people, pets, places and countries are welcome: keep them (fix the spelling if needed).
If the word is not suitable for young children, or is not a real word, pick a friendly,
similar-sounding everyday word instead and use that as "word". Never repeat rude words.`

var validWord = regexp.MustCompile(`^[A-Za-z]{1,30}$`)

type sentenceResult struct {
	Original string `json:"original"`
	Word     string `json:"word"`
	Sentence string `json:"sentence"`
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
		w.Write(indexHTML)
	})
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
	word := strings.ToLower(strings.TrimSpace(req.Word))
	if !validWord.MatchString(word) {
		http.Error(w, "letters only", http.StatusBadRequest)
		return
	}

	res, err := s.makeSentence(r.Context(), word)
	if err != nil {
		log.Printf("sentence for %q: %v", word, err)
		http.Error(w, "try again", http.StatusBadGateway)
		return
	}
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
	var out sentenceResult
	if err := json.Unmarshal([]byte(text[start:end+1]), &out); err != nil {
		return nil, fmt.Errorf("bad JSON in reply: %q", text)
	}
	out.Word = strings.ToLower(strings.TrimSpace(out.Word))
	out.Sentence = strings.TrimSpace(out.Sentence)
	if out.Word == "" || out.Sentence == "" {
		return nil, errors.New("empty word or sentence")
	}
	out.Original = word
	return &out, nil
}
