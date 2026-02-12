package constants_test

import (
	"MediaWarp/constants"
	"testing"
)

func TestEmbyDownloadHandlerRegexp(t *testing.T) {
	testCases := []struct {
		name string
		path string
		want bool
	}{
		{
			name: "带 emby 前缀",
			path: "/emby/Items/18464/Download",
			want: true,
		},
		{
			name: "不带 emby 前缀",
			path: "/Items/18464/Download",
			want: true,
		},
		{
			name: "大小写混合",
			path: "/eMbY/iTeMs/18464/dOwNlOaD",
			want: true,
		},
		{
			name: "错误接口",
			path: "/emby/Items/18464/PlaybackInfo",
			want: false,
		},
		{
			name: "非数字 itemId",
			path: "/emby/Items/abc/Download",
			want: false,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			got := constants.EmbyRegexp.Router.DownloadHandler.MatchString(testCase.path)
			if got != testCase.want {
				t.Fatalf("路径匹配结果错误: path=%s, want=%t, got=%t", testCase.path, testCase.want, got)
			}
		})
	}
}
