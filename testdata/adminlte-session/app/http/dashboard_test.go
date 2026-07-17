package pickle

import (
	"net/http/httptest"
	"path"
	"regexp"
	"strings"
	"testing"
)

func TestDashboardRendersHTMLAndEscapesData(t *testing.T) {
	request := httptest.NewRequest("GET", "/", nil)
	recorder := httptest.NewRecorder()
	ctx := NewContext(recorder, request)
	ctx.router = dashboardTestRouter()
	ctx.routeName = "dashboard"
	data := DashboardData{}
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

func TestDashboardAssetIsContentAddressedAndCacheable(t *testing.T) {
	pageRecorder := httptest.NewRecorder()
	pageContext := NewContext(pageRecorder, httptest.NewRequest("GET", "/", nil))
	pageContext.router = dashboardTestRouter()
	pageContext.routeName = "dashboard"
	data := DashboardData{}
	Dashboard(pageContext, data).Write(pageRecorder)
	match := regexp.MustCompile(`href="(/assets/[^"]+\.css)"`).FindStringSubmatch(pageRecorder.Body.String())
	if len(match) != 2 {
		t.Fatalf("content-addressed stylesheet missing: %s", pageRecorder.Body.String())
	}

	assetRecorder := httptest.NewRecorder()
	assetContext := NewContext(assetRecorder, httptest.NewRequest("GET", match[1], nil))
	assetContext.SetParam("asset", path.Base(match[1]))
	PickleAsset(assetContext).Write(assetRecorder)
	if assetRecorder.Code != 200 || !strings.Contains(assetRecorder.Body.String(), ".app-wrapper") {
		t.Fatalf("asset response = %d %q", assetRecorder.Code, assetRecorder.Body.String())
	}
	if got := assetRecorder.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Fatalf("Cache-Control = %q", got)
	}
	etag := assetRecorder.Header().Get("ETag")
	if etag == "" {
		t.Fatal("ETag missing")
	}

	notModifiedRequest := httptest.NewRequest("GET", match[1], nil)
	notModifiedRequest.Header.Set("If-None-Match", etag)
	notModifiedRecorder := httptest.NewRecorder()
	notModifiedContext := NewContext(notModifiedRecorder, notModifiedRequest)
	notModifiedContext.SetParam("asset", path.Base(match[1]))
	PickleAsset(notModifiedContext).Write(notModifiedRecorder)
	if notModifiedRecorder.Code != 304 || notModifiedRecorder.Body.Len() != 0 {
		t.Fatalf("conditional response = %d %q", notModifiedRecorder.Code, notModifiedRecorder.Body.String())
	}
}

func dashboardTestRouter() *Router {
	return Routes(func(r *Router) {
		r.Get("/", func(*Context) Response { return Response{} }).Name("dashboard")
		r.Post("/logout", func(*Context) Response { return Response{} }).Name("logout")
	})
}
