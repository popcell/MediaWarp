package handler

import (
	"MediaWarp/internal/config"
	"MediaWarp/internal/service/emby"
	"MediaWarp/utils"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

type roundTripperFunc func(req *http.Request) (*http.Response, error)

func (fn roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

type closeNotifyRecorder struct {
	*httptest.ResponseRecorder
	closeCh chan bool
}

func newCloseNotifyRecorder() *closeNotifyRecorder {
	return &closeNotifyRecorder{
		ResponseRecorder: httptest.NewRecorder(),
		closeCh:          make(chan bool, 1),
	}
}

func (recorder *closeNotifyRecorder) CloseNotify() <-chan bool {
	return recorder.closeCh
}

func newHTTPResponse(req *http.Request, status int, body string, headers map[string]string) *http.Response {
	header := make(http.Header, len(headers))
	for key, value := range headers {
		header.Set(key, value)
	}

	return &http.Response{
		StatusCode:    status,
		Status:        fmt.Sprintf("%d %s", status, http.StatusText(status)),
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        header,
		Body:          io.NopCloser(strings.NewReader(body)),
		ContentLength: int64(len(body)),
		Request:       req,
	}
}

func replaceGlobalHTTPClientTransport(t *testing.T, transport http.RoundTripper) {
	t.Helper()

	httpClient := utils.GetHTTPClient()
	oldTransport := httpClient.Transport
	httpClient.Transport = transport
	t.Cleanup(func() {
		httpClient.Transport = oldTransport
	})
}

func newTestEmbyHandler(t *testing.T, endpoint string, strmHandler StrmHandlerFunc, proxyTransport http.RoundTripper) *EmbyHandler {
	t.Helper()

	target, err := url.Parse(endpoint)
	if err != nil {
		t.Fatalf("解析测试服务器 URL 失败: %v", err)
	}

	if strmHandler == nil {
		strmHandler = func(content string, _ string) string { return content }
	}

	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport = proxyTransport

	return &EmbyHandler{
		client:          emby.New(endpoint, "test-api-key"),
		proxy:           proxy,
		httpStrmHandler: strmHandler,
	}
}

func performDownloadRequest(t *testing.T, handler *EmbyHandler, method string, requestURI string) *closeNotifyRecorder {
	t.Helper()

	recorder := newCloseNotifyRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest(method, requestURI, nil)
	req.Header.Set("User-Agent", "MediaWarp-UnitTest")
	ctx.Request = req

	handler.DownloadHandler(ctx)
	return recorder
}

func TestDownloadHandlerRedirectHTTPStrm(t *testing.T) {
	gin.SetMode(gin.TestMode)

	oldHTTPStrm := config.HTTPStrm
	oldAlistStrm := config.AlistStrm
	defer func() {
		config.HTTPStrm = oldHTTPStrm
		config.AlistStrm = oldAlistStrm
	}()

	config.HTTPStrm.Enable = true
	config.HTTPStrm.PrefixList = []string{"/strm/http/"}
	config.AlistStrm.Enable = false

	replaceGlobalHTTPClientTransport(t, roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/Items" {
			return newHTTPResponse(req, http.StatusNotFound, "not-found", nil), nil
		}
		body := `{"Items":[{"Path":"/strm/http/movie.strm","MediaSources":[{"Id":"mediasource_1","Protocol":"Http","Path":"https://origin.example/first.mp4"},{"Id":"mediasource_2","Protocol":"Http","Path":"https://origin.example/second.mp4"}]}]}`
		return newHTTPResponse(req, http.StatusOK, body, nil), nil
	}))

	handler := newTestEmbyHandler(
		t,
		"http://emby.test",
		func(content string, _ string) string { return content + "#redirected" },
		roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			return newHTTPResponse(req, http.StatusOK, "proxied", map[string]string{"X-MediaWarp-Test": "proxy"}), nil
		}),
	)

	resp := performDownloadRequest(t, handler, http.MethodGet, "/emby/Items/18464/Download?api_key=test&mediasourceid=mediasource_2")

	if resp.Code != http.StatusFound {
		t.Fatalf("状态码错误: want=%d, got=%d", http.StatusFound, resp.Code)
	}

	wantLocation := "https://origin.example/second.mp4#redirected"
	if got := resp.Header().Get("Location"); got != wantLocation {
		t.Fatalf("重定向地址错误: want=%s, got=%s", wantLocation, got)
	}
}

func TestDownloadHandlerFallbackToReverseProxy(t *testing.T) {
	gin.SetMode(gin.TestMode)

	oldHTTPStrm := config.HTTPStrm
	oldAlistStrm := config.AlistStrm
	defer func() {
		config.HTTPStrm = oldHTTPStrm
		config.AlistStrm = oldAlistStrm
	}()

	config.HTTPStrm.Enable = true
	config.HTTPStrm.PrefixList = []string{"/strm/http/"}
	config.AlistStrm.Enable = false

	testCases := []struct {
		name       string
		method     string
		requestURI string
		itemsBody  string
	}{
		{
			name:       "HEAD 请求直接回退",
			method:     http.MethodHead,
			requestURI: "/emby/Items/18464/Download?api_key=test",
			itemsBody:  "invalid-json",
		},
		{
			name:       "非 strm 文件",
			method:     http.MethodGet,
			requestURI: "/emby/Items/18464/Download?api_key=test",
			itemsBody:  `{"Items":[{"Path":"/video/movie.mkv","MediaSources":[{"Id":"mediasource_1","Protocol":"Http","Path":"https://origin.example/movie.mkv"}]}]}`,
		},
		{
			name:       "strm 但非 HTTPStrm 类型",
			method:     http.MethodGet,
			requestURI: "/emby/Items/18464/Download?api_key=test",
			itemsBody:  `{"Items":[{"Path":"/strm/other/movie.strm","MediaSources":[{"Id":"mediasource_1","Protocol":"Http","Path":"https://origin.example/movie.mkv"}]}]}`,
		},
		{
			name:       "查询结果非法 JSON",
			method:     http.MethodGet,
			requestURI: "/emby/Items/18464/Download?api_key=test",
			itemsBody:  "invalid-json",
		},
		{
			name:       "查询结果为空",
			method:     http.MethodGet,
			requestURI: "/emby/Items/18464/Download?api_key=test",
			itemsBody:  `{"Items":[]}`,
		},
		{
			name:       "item.Path 为空",
			method:     http.MethodGet,
			requestURI: "/emby/Items/18464/Download?api_key=test",
			itemsBody:  `{"Items":[{"MediaSources":[{"Id":"mediasource_1","Protocol":"Http","Path":"https://origin.example/movie.mkv"}]}]}`,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			replaceGlobalHTTPClientTransport(t, roundTripperFunc(func(req *http.Request) (*http.Response, error) {
				if req.URL.Path != "/Items" {
					return newHTTPResponse(req, http.StatusNotFound, "not-found", nil), nil
				}
				return newHTTPResponse(req, http.StatusOK, testCase.itemsBody, nil), nil
			}))

			handler := newTestEmbyHandler(
				t,
				"http://emby.test",
				func(content string, _ string) string { return content + "#redirected" },
				roundTripperFunc(func(req *http.Request) (*http.Response, error) {
					return newHTTPResponse(req, http.StatusOK, "proxied", map[string]string{"X-MediaWarp-Test": "proxy"}), nil
				}),
			)

			resp := performDownloadRequest(t, handler, testCase.method, testCase.requestURI)

			if resp.Code != http.StatusOK {
				t.Fatalf("状态码错误: want=%d, got=%d", http.StatusOK, resp.Code)
			}

			if got := resp.Header().Get("X-MediaWarp-Test"); got != "proxy" {
				t.Fatalf("未回退到反向代理: want=%s, got=%s", "proxy", got)
			}
		})
	}
}
