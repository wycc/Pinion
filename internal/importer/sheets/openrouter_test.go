package sheets

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func fakeOpenRouter(t *testing.T, status int, reply string, check func(map[string]any)) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sk-or-test" {
			t.Errorf("auth header %q", r.Header.Get("Authorization"))
		}
		b, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(b, &body)
		if check != nil {
			check(body)
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, reply)
	}))
	t.Cleanup(srv.Close)
	old := openRouterURL
	openRouterURL = srv.URL
	t.Cleanup(func() { openRouterURL = old })
}

const sampleRows = "#,name,port\n2,pc1,2001\n"

func TestOpenRouterSuccess(t *testing.T) {
	content, _ := json.Marshal(`{"sessions":[{"row":2,"name":"pc1","folder":"","host":"","port":2001,"user":""}]}`)
	fakeOpenRouter(t, 200, `{"choices":[{"finish_reason":"stop","message":{"content":`+string(content)+`}}]}`, func(b map[string]any) {
		if b["model"] != DefaultOpenRouterModel {
			t.Errorf("model %v", b["model"])
		}
		rf, _ := b["response_format"].(map[string]any)
		if rf["type"] != "json_schema" {
			t.Errorf("response_format %v", rf)
		}
	})
	got, err := ParseOpenRouter(context.Background(), "sk-or-test", "", sampleRows)
	if err != nil || len(got) != 1 || got[0].Port != 2001 || got[0].Name != "pc1" || got[0].Row != 2 {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestOpenRouterErrors(t *testing.T) {
	cases := []struct {
		status int
		reply  string
		want   string
	}{
		{401, `{"error":{"message":"No auth credentials found","code":401}}`, "key was rejected"},
		{402, `{"error":{"message":"Insufficient credits","code":402}}`, "credits"},
		{400, `{"error":{"message":"anthropic/nope is not a valid model ID","code":400}}`, "does not accept model"},
		{200, `{"error":{"message":"upstream overloaded","code":502}}`, "upstream overloaded"},
		{200, `{"choices":[{"finish_reason":"length","message":{"content":"{\"sess"}}]}`, "too many rows"},
	}
	for _, c := range cases {
		fakeOpenRouter(t, c.status, c.reply, nil)
		_, err := ParseOpenRouter(context.Background(), "sk-or-test", "", sampleRows)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("HTTP %d %s: err %v, want %q", c.status, c.reply, err, c.want)
		}
	}
}
