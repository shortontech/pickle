package pickle

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDashboardRendersHTMLAndEscapesData(t *testing.T) {
	request := httptest.NewRequest("GET", "/", nil)
	recorder := httptest.NewRecorder()
	ctx := NewContext(recorder, request)
	data := DashboardData{Authenticated: true}
	data.Page.Title = `<script>alert("title")</script>`
	data.Page.Heading = "Dashboard"
	data.User.Name = `<img src=x onerror=alert(1)>`
	Dashboard(ctx, data).Write(recorder)

	if got := recorder.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Fatalf("Content-Type = %q", got)
	}
	body := recorder.Body.String()
	if strings.Contains(body, "<script>") || strings.Contains(body, "<img") {
		t.Fatalf("unescaped template data: %s", body)
	}
	if !strings.Contains(body, "&lt;script&gt;") || !strings.Contains(body, "&lt;img") {
		t.Fatalf("escaped values missing: %s", body)
	}
}
