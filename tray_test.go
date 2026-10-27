package main

import (
	"my-video-go/internal/models"
	"testing"
)

// 状态项文字：托盘菜单里那一行"共享中：http://…"。
//
// 这几条断言的都是**用户看到的那一串字**，而不是内部字段——写错的效果是菜单里
// 显示一个点不开的地址，没人会为此开 issue。
func TestTrayStatusLabel(t *testing.T) {
	cases := []struct {
		name string
		info *models.ShareServerInfo
		want string
	}{
		{"未共享（nil）", nil, ""},
		{"端口为 0 视为未运行", &models.ShareServerInfo{IPs: []string{"192.168.1.5"}}, ""},
		{
			"正常：取第一个 IP",
			&models.ShareServerInfo{IPs: []string{"192.168.1.5", "10.0.0.2"}, Port: 6008},
			"共享中：http://192.168.1.5:6008",
		},
		{
			// 拿不到本机地址时不能编一个：写"共享中"仍然回答了最关键的问题
			"没有 IP 时只写共享中",
			&models.ShareServerInfo{IPs: nil, Port: 6008},
			"共享中",
		},
	}

	for _, c := range cases {
		if got := trayStatusLabel(c.info); got != c.want {
			t.Errorf("%s：得到 %q，期望 %q", c.name, got, c.want)
		}
	}
}

// 关窗口的语义：**关窗口不改变共享状态**。
//
// 这条是托盘的整个设计支点，写成纯函数是为了能这样直接断言：
//   - 共享开着 → 隐藏（关窗口的本意通常只是"收起来"，不该掐断家人的播放）
//   - 共享没开 → 退出（用户就是想关掉，留后台只会让人去任务管理器）
//   - 正在退出 → 放行（否则托盘"退出"会被自己拦成"藏起来"）
func TestShouldHideOnClose(t *testing.T) {
	cases := []struct {
		name     string
		sharing  bool
		quitting bool
		want     bool
	}{
		{"共享中、正常关窗 -> 隐藏", true, false, true},
		{"未共享、正常关窗 -> 退出", false, false, false},
		{"共享中但正在退出 -> 放行", true, true, false},
		{"未共享且正在退出 -> 放行", false, true, false},
	}

	for _, c := range cases {
		if got := shouldHideOnClose(c.sharing, c.quitting); got != c.want {
			t.Errorf("%s：得到 %v，期望 %v", c.name, got, c.want)
		}
	}
}

func TestTrayTooltip(t *testing.T) {
	if got := trayTooltip(true); got != "视频扫描器 — 共享中" {
		t.Errorf("共享中的 tooltip 不对：%q", got)
	}
	if got := trayTooltip(false); got != "视频扫描器" {
		t.Errorf("未共享的 tooltip 不对：%q", got)
	}
}
