package telegram

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSendKeyboard(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":4242}}`))
	}))
	defer srv.Close()

	kb := [][]InlineButton{{{Text: "✅", CallbackData: "a:7"}, {Text: "❌", CallbackData: "r:7"}}}
	id, err := New(srv.URL, "T", srv.Client()).SendKeyboard(context.Background(), "55", "proposal", kb)
	if err != nil {
		t.Fatalf("SendKeyboard: %v", err)
	}
	if id != 4242 {
		t.Errorf("message id = %d, want 4242", id)
	}
	if gotPath != "/botT/sendMessage" {
		t.Errorf("path = %q", gotPath)
	}
	// reply_markup.inline_keyboard[0] must carry the two callback_data values.
	rm, _ := gotBody["reply_markup"].(map[string]any)
	ik, _ := rm["inline_keyboard"].([]any)
	if len(ik) != 1 {
		t.Fatalf("inline_keyboard rows = %d, want 1", len(ik))
	}
	row, _ := ik[0].([]any)
	if len(row) != 2 {
		t.Fatalf("buttons = %d, want 2", len(row))
	}
	b0, _ := row[0].(map[string]any)
	if b0["callback_data"] != "a:7" {
		t.Errorf("button0 callback_data = %v, want a:7", b0["callback_data"])
	}
}

func TestAnswerCallback(t *testing.T) {
	var path string
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &body)
		_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
	}))
	defer srv.Close()
	if err := New(srv.URL, "T", srv.Client()).AnswerCallback(context.Background(), "cb1", "Approved"); err != nil {
		t.Fatalf("AnswerCallback: %v", err)
	}
	if path != "/botT/answerCallbackQuery" {
		t.Errorf("path = %q", path)
	}
	if body["callback_query_id"] != "cb1" || body["text"] != "Approved" {
		t.Errorf("body = %v", body)
	}
}

func TestEditMessageText(t *testing.T) {
	var path string
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &body)
		_, _ = w.Write([]byte(`{"ok":true,"result":{}}`))
	}))
	defer srv.Close()
	if err := New(srv.URL, "T", srv.Client()).EditMessageText(context.Background(), "55", 4242, "✅ Approved"); err != nil {
		t.Fatalf("EditMessageText: %v", err)
	}
	if path != "/botT/editMessageText" {
		t.Errorf("path = %q", path)
	}
	if body["text"] != "✅ Approved" || body["message_id"].(float64) != 4242 {
		t.Errorf("body = %v", body)
	}
	// Buttons stripped: empty inline_keyboard.
	rm, _ := body["reply_markup"].(map[string]any)
	if ik, _ := rm["inline_keyboard"].([]any); len(ik) != 0 {
		t.Errorf("inline_keyboard = %v, want empty", ik)
	}
}

func TestGetUpdates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"result":[
			{"update_id":10,"callback_query":{"id":"cb1","data":"a:7","from":{"id":1,"username":"me"},"message":{"message_id":4242,"chat":{"id":55}}}},
			{"update_id":11,"message":{"message_id":5,"text":"/halt","from":{"username":"me"},"chat":{"id":55}}}
		]}`))
	}))
	defer srv.Close()

	ups, err := New(srv.URL, "T", srv.Client()).GetUpdates(context.Background(), 0, 1)
	if err != nil {
		t.Fatalf("GetUpdates: %v", err)
	}
	if len(ups) != 2 {
		t.Fatalf("updates = %d, want 2", len(ups))
	}
	if ups[0].CallbackQuery == nil || ups[0].CallbackQuery.Data != "a:7" {
		t.Errorf("callback = %+v", ups[0].CallbackQuery)
	}
	if ups[1].Message == nil || ups[1].Message.Text != "/halt" {
		t.Errorf("message = %+v", ups[1].Message)
	}
}
