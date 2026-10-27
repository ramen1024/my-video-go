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

// 关窗口的语义：**一律隐藏，只有"正在退出"时放行**。
//
// 这条曾经按"共享是否在运行"分流，被真实反馈推翻过一次：用户关窗口前没法
// 预测这次是隐藏还是退出（它取决于一个与关窗口无关的状态），于是"托盘好像
// 没生效"。现在 X 只有一个含义——收起来。
func TestShouldHideOnClose(t *testing.T) {
	if !shouldHideOnClose(false) {
		t.Error("正常关窗应当隐藏到托盘")
	}
	if shouldHideOnClose(true) {
		// 不放行的话，托盘「退出」会先撞上这条规则被拦成"藏起来"
		t.Error("正在退出时不应再隐藏（否则退出变藏起来）")
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
